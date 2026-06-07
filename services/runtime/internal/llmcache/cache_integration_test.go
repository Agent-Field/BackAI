//go:build integration

// Integration tests for the llmcache package. These run against a live
// Postgres instance and cover the actual cache mechanics — Put/Get
// round-trip, hit accounting, tenant isolation, expiry, eviction, and
// stats math.
//
// To run:
//
//	docker run --rm -d --name af-llmcache-pg \
//	  -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:17-alpine
//	export JOBS_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable'
//	go test -tags=integration ./services/runtime/internal/llmcache/...
//
// We reuse the JOBS_TEST_DATABASE_URL env name to keep the CI matrix
// free of new variables. Each test gets its own ephemeral schema so
// runs against a shared DB don't collide.

package llmcache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const databaseURLEnv = "JOBS_TEST_DATABASE_URL"

// testCacheSetup creates an ephemeral schema, installs the suite_llm_cache
// table inline (no goose import — that would tangle this package with
// the migrations FS), and returns a fresh Cache wired to a pool that
// pins every connection to the new schema.
func testCacheSetup(t *testing.T) (*Cache, *pgxpool.Pool, func()) {
	t.Helper()
	dsn := os.Getenv(databaseURLEnv)
	if dsn == "" {
		t.Skipf("set %s to run llmcache integration tests", databaseURLEnv)
	}

	rb := make([]byte, 6)
	if _, err := rand.Read(rb); err != nil {
		t.Fatal(err)
	}
	schema := "llmcache_test_" + hex.EncodeToString(rb)

	ctx := context.Background()
	boot, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
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
		_, err := c.Exec(ctx, fmt.Sprintf(`set search_path to %q`, schema))
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}

	// Inline schema — mirror of migration 00006_llmcache.sql. We don't
	// import the migrations FS here because that would import package
	// `db` and force the test binary to carry every other migration.
	const schemaSQL = `
create table if not exists suite_llm_cache (
  key              text primary key,
  tenant_id        uuid,
  model            text not null,
  request_hash     text not null,
  response_json    jsonb not null,
  prompt_tokens    int not null default 0,
  completion_tokens int not null default 0,
  hits             int not null default 0,
  cost_saved_usd   numeric(12,6) not null default 0,
  created_at       timestamptz not null default now(),
  expires_at       timestamptz not null,
  last_hit_at      timestamptz
);
create index if not exists suite_llm_cache_expires_idx
  on suite_llm_cache (expires_at);
create index if not exists suite_llm_cache_tenant_model_idx
  on suite_llm_cache (tenant_id, model);
`
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		t.Fatalf("install schema: %v", err)
	}

	c, err := New(pool, slog.New(slog.NewTextHandler(os.Stderr,
		&slog.HandlerOptions{Level: slog.LevelWarn})))
	if err != nil {
		pool.Close()
		t.Fatalf("new cache: %v", err)
	}

	cleanup := func() {
		pool.Close()
		drop, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return
		}
		_, _ = drop.Exec(ctx, fmt.Sprintf(`drop schema %q cascade`, schema))
		drop.Close(ctx)
	}
	return c, pool, cleanup
}

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
	model   = "anthropic/claude-haiku-4-5-20251001"
)

// Put then Get returns a hit with hits=1, increments on subsequent
// Gets, and updates last_hit_at.
func TestPutAndGetIncrementsHits(t *testing.T) {
	c, _, cleanup := testCacheSetup(t)
	defer cleanup()

	ctx := context.Background()
	hash := "h-basic"
	response := []byte(`{"id":"chatcmpl-x","choices":[]}`)

	if err := c.Put(ctx, tenantA, model, hash, response, 100, 50, time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// First Get → hit, hits = 1
	entry, hit, err := c.Get(ctx, tenantA, model, hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !hit {
		t.Fatal("expected hit, got miss")
	}
	if entry.Hits != 1 {
		t.Errorf("hits=%d, want 1", entry.Hits)
	}
	if entry.LastHitAt == nil {
		t.Error("LastHitAt nil after first hit")
	}
	if string(entry.ResponseJSON) == "" {
		t.Error("ResponseJSON empty")
	}

	// Second Get → hits = 2 and cost_saved_usd grows.
	entry2, hit2, err := c.Get(ctx, tenantA, model, hash)
	if err != nil {
		t.Fatalf("Get2: %v", err)
	}
	if !hit2 {
		t.Fatal("expected hit on second call")
	}
	if entry2.Hits != 2 {
		t.Errorf("hits=%d, want 2", entry2.Hits)
	}
	if entry2.CostSavedUSD <= entry.CostSavedUSD {
		t.Errorf("cost_saved_usd did not grow: %f -> %f",
			entry.CostSavedUSD, entry2.CostSavedUSD)
	}
}

// A different request hash must miss even when (tenant, model) match.
func TestGetMissesOnDifferentHash(t *testing.T) {
	c, _, cleanup := testCacheSetup(t)
	defer cleanup()

	ctx := context.Background()
	if err := c.Put(ctx, tenantA, model, "hash-a", []byte(`{"r":1}`), 10, 5, time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	_, hit, err := c.Get(ctx, tenantA, model, "hash-b")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Error("expected miss for different hash, got hit")
	}
}

// Tenant A's cached response must NOT appear for tenant B even when
// model + hash are identical.
func TestTenantIsolation(t *testing.T) {
	c, _, cleanup := testCacheSetup(t)
	defer cleanup()

	ctx := context.Background()
	hash := "shared-hash"
	if err := c.Put(ctx, tenantA, model, hash, []byte(`{"a":1}`), 10, 5, time.Hour); err != nil {
		t.Fatalf("Put A: %v", err)
	}
	// Tenant B looks up the SAME hash → must miss.
	_, hit, err := c.Get(ctx, tenantB, model, hash)
	if err != nil {
		t.Fatalf("Get B: %v", err)
	}
	if hit {
		t.Fatal("tenant isolation broken: B saw A's entry")
	}

	// B now stores its own value under the same hash.
	if err := c.Put(ctx, tenantB, model, hash, []byte(`{"b":2}`), 20, 10, time.Hour); err != nil {
		t.Fatalf("Put B: %v", err)
	}
	entryA, hitA, _ := c.Get(ctx, tenantA, model, hash)
	entryB, hitB, _ := c.Get(ctx, tenantB, model, hash)
	if !hitA || !hitB {
		t.Fatal("expected both tenants to hit their own row")
	}
	if string(entryA.ResponseJSON) == string(entryB.ResponseJSON) {
		t.Error("tenants returned the same response payload")
	}
}

// Expired entries must miss, and Evict must remove them.
func TestExpiryAndEviction(t *testing.T) {
	c, pool, cleanup := testCacheSetup(t)
	defer cleanup()

	ctx := context.Background()
	hash := "h-expiry"
	if err := c.Put(ctx, tenantA, model, hash, []byte(`{"x":1}`), 10, 5, time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Force the row's expires_at into the past — much faster than
	// sleeping out the TTL.
	key := CacheKey(tenantA, model, hash)
	if _, err := pool.Exec(ctx,
		`update suite_llm_cache set expires_at = now() - interval '1 minute' where key = $1`,
		key,
	); err != nil {
		t.Fatalf("expire: %v", err)
	}

	_, hit, err := c.Get(ctx, tenantA, model, hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Fatal("expected miss on expired row")
	}

	removed, err := c.Evict(ctx)
	if err != nil {
		t.Fatalf("Evict: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 row evicted, got %d", removed)
	}

	// Row is really gone.
	var count int
	_ = pool.QueryRow(ctx, `select count(*) from suite_llm_cache where key = $1`, key).Scan(&count)
	if count != 0 {
		t.Errorf("row not deleted, count=%d", count)
	}
}

// Stats math: when CacheMisses is fed in by the caller (or left at 0),
// hit_rate = hits / (hits + misses). We exercise the in-package path
// (misses = 0 → hit_rate = 1) and the math sanity-check.
func TestStatsMath(t *testing.T) {
	c, _, cleanup := testCacheSetup(t)
	defer cleanup()

	ctx := context.Background()
	// Put + 3 hits on entry 1.
	if err := c.Put(ctx, tenantA, model, "s-1", []byte(`{"a":1}`), 100, 50, time.Hour); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, _, err := c.Get(ctx, tenantA, model, "s-1"); err != nil {
			t.Fatalf("Get 1: %v", err)
		}
	}

	// Put + 1 hit on entry 2.
	if err := c.Put(ctx, tenantA, model, "s-2", []byte(`{"b":2}`), 200, 100, time.Hour); err != nil {
		t.Fatalf("Put 2: %v", err)
	}
	if _, _, err := c.Get(ctx, tenantA, model, "s-2"); err != nil {
		t.Fatalf("Get 2: %v", err)
	}

	stats, err := c.Stats(ctx, tenantA)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.CacheHits != 4 {
		t.Errorf("hits=%d, want 4", stats.CacheHits)
	}
	if stats.Entries != 2 {
		t.Errorf("entries=%d, want 2", stats.Entries)
	}
	if stats.SavingsUSD <= 0 {
		t.Errorf("savings=%f, want > 0 (priced model)", stats.SavingsUSD)
	}
	// With misses = 0, hit_rate should be exactly 1.
	if stats.HitRate != 1 {
		t.Errorf("hit_rate=%f, want 1", stats.HitRate)
	}

	// Stats for tenant B must report zero hits (isolation).
	statsB, err := c.Stats(ctx, tenantB)
	if err != nil {
		t.Fatalf("Stats B: %v", err)
	}
	if statsB.CacheHits != 0 || statsB.Entries != 0 {
		t.Errorf("tenant B sees A's stats: %+v", statsB)
	}

	// Global stats (empty tenant) sums across.
	statsAll, err := c.Stats(ctx, "")
	if err != nil {
		t.Fatalf("Stats all: %v", err)
	}
	if statsAll.CacheHits != 4 {
		t.Errorf("global hits=%d, want 4", statsAll.CacheHits)
	}
}

// A second Put to the same key refreshes the response + expiry but
// preserves the hit counter. This is the "stale row replaced" path.
func TestPutOnConflictPreservesHits(t *testing.T) {
	c, pool, cleanup := testCacheSetup(t)
	defer cleanup()

	ctx := context.Background()
	hash := "h-conflict"
	if err := c.Put(ctx, tenantA, model, hash, []byte(`{"v":1}`), 10, 5, time.Hour); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	// One hit.
	if _, _, err := c.Get(ctx, tenantA, model, hash); err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Re-put with new response + tokens.
	if err := c.Put(ctx, tenantA, model, hash, []byte(`{"v":2}`), 99, 99, 2*time.Hour); err != nil {
		t.Fatalf("Put 2: %v", err)
	}

	// hits should still be 1 (Put doesn't clear them).
	var hits int
	var promptTokens int
	if err := pool.QueryRow(ctx,
		`select hits, prompt_tokens from suite_llm_cache where key = $1`,
		CacheKey(tenantA, model, hash),
	).Scan(&hits, &promptTokens); err != nil {
		t.Fatalf("introspect: %v", err)
	}
	if hits != 1 {
		t.Errorf("hits=%d after re-Put, want 1", hits)
	}
	if promptTokens != 99 {
		t.Errorf("prompt_tokens=%d, want 99 (re-Put should refresh)", promptTokens)
	}
}

// Anonymous (empty tenant) Put + Get must round-trip — the gateway uses
// this path for unauthenticated callers and system jobs.
func TestAnonymousTenantRoundTrip(t *testing.T) {
	c, _, cleanup := testCacheSetup(t)
	defer cleanup()

	ctx := context.Background()
	if err := c.Put(ctx, "", model, "h-anon", []byte(`{"a":1}`), 10, 5, time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	entry, hit, err := c.Get(ctx, "", model, "h-anon")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !hit {
		t.Fatal("anon: expected hit")
	}
	if entry.TenantID != "" {
		t.Errorf("expected empty tenant_id, got %q", entry.TenantID)
	}
}
