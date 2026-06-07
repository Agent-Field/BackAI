// SPDX-License-Identifier: Apache-2.0

//go:build integration

// Integration tests for the suite memory store.
//
// Gated behind the `integration` build tag so `go test ./...` stays
// fast on developer machines without Postgres. Mirrors the llmcache
// / cost packages (see their *_test.go for the same setup pattern).
//
// To run:
//
//	docker run --rm -d --name af-memory-pg \
//	  -e POSTGRES_PASSWORD=postgres -p 5432:5432 pgvector/pgvector:pg16
//	export JOBS_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable'
//	go test -tags=integration ./services/runtime/internal/memory/...
//
// The image MUST be pgvector/pgvector:pg16 (or compatible) — the
// CREATE EXTENSION vector statement will fail otherwise.

package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

const databaseURLEnv = "JOBS_TEST_DATABASE_URL"

// testSetup creates an ephemeral schema, installs suite_memory inline,
// and returns a Store wired to a pool pinned to the new schema. The
// pgvector extension is created at the database level (it's
// per-database, not per-schema) — re-runs are no-ops.
func testSetup(t *testing.T, embedder Embedder) (*Store, *pgxpool.Pool, func()) {
	t.Helper()
	dsn := os.Getenv(databaseURLEnv)
	if dsn == "" {
		t.Skipf("set %s to run memory integration tests", databaseURLEnv)
	}

	rb := make([]byte, 6)
	if _, err := rand.Read(rb); err != nil {
		t.Fatal(err)
	}
	schema := "memory_test_" + hex.EncodeToString(rb)

	ctx := context.Background()
	boot, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := boot.Exec(ctx, `create extension if not exists vector`); err != nil {
		boot.Close(ctx)
		t.Fatalf("create extension vector: %v (image must be pgvector/pgvector)", err)
	}
	if _, err := boot.Exec(ctx, fmt.Sprintf(`create schema %q`, schema)); err != nil {
		boot.Close(ctx)
		t.Fatalf("create schema: %v", err)
	}
	boot.Close(ctx)

	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pcfg.MaxConns = 4
	pcfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, fmt.Sprintf(`set search_path to %q, public`, schema))
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}

	const schemaSQL = `
create table suite_memory (
  scope text not null,
  scope_id text not null default '',
  key text not null,
  value jsonb not null,
  metadata jsonb not null default '{}'::jsonb,
  embedding vector(1536),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (scope, scope_id, key)
);
create index suite_memory_scope_idx on suite_memory (scope, scope_id);
`
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		t.Fatalf("schema: %v", err)
	}

	store, err := New(pool, embedder, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cleanup := func() {
		pool.Close()
		conn, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			return
		}
		_, _ = conn.Exec(context.Background(), fmt.Sprintf(`drop schema if exists %q cascade`, schema))
		conn.Close(context.Background())
	}
	return store, pool, cleanup
}

// fakeEmbedder is a deterministic embedder for tests. It hashes the
// input text into a stable seed and emits a unit-normalised vector
// where every component is derived from that seed. Identical inputs
// yield identical vectors (similarity ≈ 1.0); different inputs yield
// different vectors with bounded similarity.
type fakeEmbedder struct {
	calls int
	mu    sync.Mutex
	fail  bool // when true, Embed returns an error
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	f.mu.Lock()
	f.calls++
	failNow := f.fail
	f.mu.Unlock()
	if failNow {
		return nil, errors.New("fake embedder forced failure")
	}
	return makeVector(text), nil
}

func (f *fakeEmbedder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// makeVector turns text into a deterministic, unit-norm 1536-vector.
// Each component is a sine of a hash-derived phase so the vector lies
// on the unit hyper-sphere. Identical text yields identical vectors;
// different text yields different vectors.
func makeVector(text string) []float32 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	seed := h.Sum64()
	vec := make([]float32, EmbeddingDim)
	var norm float64
	for i := range vec {
		// Spread components across [-1, 1] using sine of phase derived
		// from the seed and the index. This is intentionally not
		// random — the test relies on identical inputs producing
		// identical vectors.
		phase := float64(seed%1000000) + float64(i)*0.123
		v := math.Sin(phase / 1000.0)
		vec[i] = float32(v)
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		norm = 1
	}
	for i := range vec {
		vec[i] = float32(float64(vec[i]) / norm)
	}
	return vec
}

// ─── Put / Get round-trip ────────────────────────────────────────────────

func TestPutGetRoundTrip(t *testing.T) {
	store, _, cleanup := testSetup(t, nil)
	defer cleanup()

	ctx := context.Background()
	in := PutInput{
		Scope:    ScopeGlobal,
		Key:      "fav-color",
		Value:    json.RawMessage(`"orange"`),
		Metadata: map[string]any{"source": "test"},
	}
	entry, err := store.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if entry.Scope != ScopeGlobal {
		t.Errorf("scope=%q want global", entry.Scope)
	}
	if entry.ScopeID != nil {
		t.Errorf("scope_id should be nil for global, got %v", entry.ScopeID)
	}
	if string(entry.Value) != `"orange"` {
		t.Errorf("value=%q want \"orange\"", entry.Value)
	}
	if entry.HasEmbedding {
		t.Errorf("expected no embedding (embed=false)")
	}

	got, err := store.Get(ctx, ScopeGlobal, "", "fav-color")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Value) != `"orange"` {
		t.Errorf("Get returned %q", got.Value)
	}
	if got.Metadata["source"] != "test" {
		t.Errorf("metadata lost: %+v", got.Metadata)
	}
}

func TestGetNotFound(t *testing.T) {
	store, _, cleanup := testSetup(t, nil)
	defer cleanup()

	_, err := store.Get(context.Background(), ScopeGlobal, "", "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ─── Scope isolation ─────────────────────────────────────────────────────

func TestScopeIDIsolation(t *testing.T) {
	store, _, cleanup := testSetup(t, nil)
	defer cleanup()

	ctx := context.Background()
	// Same scope + key under different scope_ids must remain distinct.
	_, err := store.Put(ctx, PutInput{
		Scope:   ScopeAgent,
		ScopeID: "agent.a",
		Key:     "state",
		Value:   json.RawMessage(`"alpha"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Put(ctx, PutInput{
		Scope:   ScopeAgent,
		ScopeID: "agent.b",
		Key:     "state",
		Value:   json.RawMessage(`"beta"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := store.Get(ctx, ScopeAgent, "agent.a", "state")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Get(ctx, ScopeAgent, "agent.b", "state")
	if err != nil {
		t.Fatal(err)
	}
	if string(a.Value) != `"alpha"` || string(b.Value) != `"beta"` {
		t.Errorf("values bled across scope_ids: a=%q b=%q", a.Value, b.Value)
	}
}

// ─── List + prefix filter ────────────────────────────────────────────────

func TestListPrefixFilter(t *testing.T) {
	store, _, cleanup := testSetup(t, nil)
	defer cleanup()

	ctx := context.Background()
	for _, k := range []string{"prefs/lang", "prefs/theme", "session/id"} {
		if _, err := store.Put(ctx, PutInput{
			Scope: ScopeGlobal,
			Key:   k,
			Value: json.RawMessage(`true`),
		}); err != nil {
			t.Fatalf("seed %q: %v", k, err)
		}
	}

	out, err := store.List(ctx, ListOpts{Prefix: "prefs/"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if out.Total != 2 {
		t.Errorf("expected total=2, got %d", out.Total)
	}
	for _, e := range out.Entries {
		if e.Key != "prefs/lang" && e.Key != "prefs/theme" {
			t.Errorf("unexpected key %q in prefix result", e.Key)
		}
	}
}

// ─── Search — identical text scores ≈ 1.0 ────────────────────────────────

func TestSearchTopK(t *testing.T) {
	emb := &fakeEmbedder{}
	store, _, cleanup := testSetup(t, emb)
	defer cleanup()

	ctx := context.Background()
	docs := []struct{ key, value string }{
		{"d1", "the quick brown fox"},
		{"d2", "lorem ipsum dolor sit amet"},
		{"d3", "a totally unrelated cooking recipe"},
	}
	for _, d := range docs {
		body, _ := json.Marshal(d.value)
		if _, err := store.Put(ctx, PutInput{
			Scope: ScopeGlobal,
			Key:   d.key,
			Value: body,
			Embed: true,
		}); err != nil {
			t.Fatalf("seed %q: %v", d.key, err)
		}
	}

	res, err := store.Search(ctx, SearchRequest{
		Query: "the quick brown fox",
		TopK:  3,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatalf("expected hits, got 0")
	}
	if res.Hits[0].Entry.Key != "d1" {
		t.Errorf("expected first hit d1, got %s", res.Hits[0].Entry.Key)
	}
	// Identical strings -> distance ≈ 0 -> similarity ≈ 1.0.
	if res.Hits[0].Similarity < 0.99 {
		t.Errorf("expected similarity≈1.0 for exact match, got %v", res.Hits[0].Similarity)
	}
}

// ─── Threshold filter ────────────────────────────────────────────────────

func TestSearchThresholdFilter(t *testing.T) {
	emb := &fakeEmbedder{}
	store, _, cleanup := testSetup(t, emb)
	defer cleanup()

	ctx := context.Background()
	// Two unrelated documents.
	for _, kv := range [][2]string{
		{"d1", "alpha"},
		{"d2", "beta"},
	} {
		body, _ := json.Marshal(kv[1])
		if _, err := store.Put(ctx, PutInput{
			Scope: ScopeGlobal,
			Key:   kv[0],
			Value: body,
			Embed: true,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	threshold := 0.999
	res, err := store.Search(ctx, SearchRequest{
		Query:     "gamma",
		Threshold: &threshold,
		TopK:      10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// "gamma" doesn't match alpha or beta -> all similarities below
	// the 0.999 threshold -> no hits.
	if len(res.Hits) != 0 {
		t.Errorf("expected 0 hits above threshold, got %d", len(res.Hits))
	}
}

// ─── Embedder failure doesn't drop the entry ─────────────────────────────

func TestPutEmbedderFailureStoresEntry(t *testing.T) {
	emb := &fakeEmbedder{fail: true}
	store, pool, cleanup := testSetup(t, emb)
	defer cleanup()

	ctx := context.Background()
	entry, err := store.Put(ctx, PutInput{
		Scope: ScopeGlobal,
		Key:   "k",
		Value: json.RawMessage(`"v"`),
		Embed: true,
	})
	if err != nil {
		t.Fatalf("Put should succeed even when embedder fails: %v", err)
	}
	if entry.HasEmbedding {
		t.Errorf("entry should have no embedding when embedder failed")
	}

	// Verify the row landed in the DB.
	var count int
	if err := pool.QueryRow(ctx,
		`select count(*) from suite_memory where scope='global' and key='k'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

// ─── No embedder, Embed=true: write succeeds without embedding ───────────

func TestPutNoEmbedderWithEmbedFlag(t *testing.T) {
	store, _, cleanup := testSetup(t, nil)
	defer cleanup()

	ctx := context.Background()
	entry, err := store.Put(ctx, PutInput{
		Scope: ScopeGlobal,
		Key:   "k",
		Value: json.RawMessage(`"v"`),
		Embed: true,
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if entry.HasEmbedding {
		t.Errorf("expected no embedding when no embedder configured")
	}
}

// ─── Tenant auto-fill from context ───────────────────────────────────────

func TestTenantAutoScope(t *testing.T) {
	store, _, cleanup := testSetup(t, nil)
	defer cleanup()

	ctx := tenantctx.WithTenant(context.Background(), "tenant-xyz", "")
	entry, err := store.Put(ctx, PutInput{
		Scope: ScopeTenant,
		Key:   "settings/theme",
		Value: json.RawMessage(`"dark"`),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if entry.ScopeID == nil || *entry.ScopeID != "tenant-xyz" {
		t.Errorf("expected auto-filled scope_id=tenant-xyz, got %+v", entry.ScopeID)
	}

	// Get with no scope_id should also auto-fill from ctx.
	got, err := store.Get(ctx, ScopeTenant, "", "settings/theme")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ScopeID == nil || *got.ScopeID != "tenant-xyz" {
		t.Errorf("Get returned wrong scope_id: %+v", got.ScopeID)
	}
}

// ─── Delete ──────────────────────────────────────────────────────────────

func TestDelete(t *testing.T) {
	store, _, cleanup := testSetup(t, nil)
	defer cleanup()

	ctx := context.Background()
	if _, err := store.Put(ctx, PutInput{
		Scope: ScopeGlobal,
		Key:   "k",
		Value: json.RawMessage(`1`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, ScopeGlobal, "", "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Delete(ctx, ScopeGlobal, "", "k"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound on second delete, got %v", err)
	}
}