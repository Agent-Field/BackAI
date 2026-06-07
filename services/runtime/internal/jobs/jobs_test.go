// SPDX-License-Identifier: Apache-2.0

//go:build integration

// Integration tests for the jobs package. These exercise the full River
// stack against a live Postgres instance and are gated behind the
// `integration` build tag so `go test ./...` stays fast on developer
// machines that don't have PG running.
//
// To run:
//
//	# 1. Start Postgres locally (any way you like). The simplest:
//	docker run --rm -d --name af-jobs-pg \
//	  -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:17-alpine
//
//	# 2. Point JOBS_TEST_DATABASE_URL at it and run the tagged suite:
//	export JOBS_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable'
//	go test -tags=integration ./services/runtime/internal/jobs/...
//
// Each test grabs a unique schema name (jobs_test_<rand>) so two runs
// against the same DB don't collide. The schema is dropped on test
// completion via t.Cleanup.
//
// What's covered:
//   - migrations (River's own schema applies cleanly on a fresh schema)
//   - Enqueue + Work end-to-end against the built-in sample-job
//   - List filtering by state and name
//   - Retry transitions a discarded job back to retryable
//   - Summary counts make sense after a flurry of inserts
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/rivertype"
)

const databaseURLEnv = "JOBS_TEST_DATABASE_URL"

// testManagerSetup opens a fresh pgx pool against an ephemeral schema and
// returns a started Manager plus a cleanup func.
func testManagerSetup(t *testing.T, regs ...func(*Manager) error) (*Manager, *pgxpool.Pool, func()) {
	t.Helper()
	dsn := os.Getenv(databaseURLEnv)
	if dsn == "" {
		t.Skipf("set %s to run jobs integration tests", databaseURLEnv)
	}

	// Each test gets its own schema so they don't share state.
	rb := make([]byte, 6)
	if _, err := rand.Read(rb); err != nil {
		t.Fatal(err)
	}
	schema := "jobs_test_" + hex.EncodeToString(rb)

	ctx := context.Background()
	bootstrap, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect bootstrap: %v", err)
	}
	if _, err := bootstrap.Exec(ctx, fmt.Sprintf("create schema %q", schema)); err != nil {
		bootstrap.Close(ctx)
		t.Fatalf("create schema: %v", err)
	}
	bootstrap.Close(ctx)

	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool: %v", err)
	}
	// Force every connection in the pool to use the test schema.
	pcfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, fmt.Sprintf("set search_path to %q", schema))
		return err
	}
	pcfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}

	if err := MigrateUp(ctx, pool, schema); err != nil {
		pool.Close()
		t.Fatalf("migrate up: %v", err)
	}

	mgr, err := NewManager(Config{
		Pool:   pool,
		Schema: schema,
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err != nil {
		pool.Close()
		t.Fatalf("new manager: %v", err)
	}
	for _, reg := range regs {
		if err := reg(mgr); err != nil {
			pool.Close()
			t.Fatalf("register: %v", err)
		}
	}

	startCtx, startCancel := context.WithTimeout(ctx, 10*time.Second)
	defer startCancel()
	if err := mgr.Start(startCtx, true, 5); err != nil {
		pool.Close()
		t.Fatalf("start: %v", err)
	}

	cleanup := func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = mgr.Stop(stopCtx)
		// Drop the schema to leave the DB clean. We acquire a fresh
		// connection because pool connections still have the search_path
		// set to the schema we're trying to drop.
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if conn, err := pgx.Connect(dropCtx, dsn); err == nil {
			_, _ = conn.Exec(dropCtx, fmt.Sprintf("drop schema if exists %q cascade", schema))
			conn.Close(dropCtx)
		}
		pool.Close()
		// Reset the package-level alias slice so the next test starts clean.
		setAliases(nil)
	}
	return mgr, pool, cleanup
}

// TestSampleJobEndToEnd registers the built-in sample-job, enqueues one,
// and waits for it to complete. Proves the full stack works.
func TestSampleJobEndToEnd(t *testing.T) {
	var ran atomic.Bool
	register := func(m *Manager) error {
		return m.Registry().RegisterGo(Definition{
			Name:        "sample-job",
			Language:    LanguageGo,
			Description: "sample (integration test)",
			MaxAttempts: 3,
		}, func(ctx context.Context, args []byte) error {
			var payload struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(args, &payload)
			if payload.Message != "hello" {
				return fmt.Errorf("unexpected message %q", payload.Message)
			}
			ran.Store(true)
			return nil
		})
	}
	mgr, _, cleanup := testManagerSetup(t, register)
	defer cleanup()

	ctx := context.Background()
	row, err := mgr.Enqueue(ctx, "sample-job", json.RawMessage(`{"message":"hello"}`), EnqueueOpts{})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if row.Kind != "sample-job" {
		t.Errorf("kind = %q, want sample-job", row.Kind)
	}
	if row.State != rivertype.JobStateAvailable {
		t.Errorf("state = %q, want available", row.State)
	}

	// Poll until done or timeout.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		got, err := mgr.Get(ctx, row.ID)
		if err == nil && got.State == rivertype.JobStateCompleted {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ran.Load() {
		t.Fatalf("handler did not run within deadline")
	}
	final, err := mgr.Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("get final: %v", err)
	}
	if final.State != rivertype.JobStateCompleted {
		t.Errorf("final state = %q, want completed (errors=%v)", final.State, final.Errors)
	}
}

// TestListFiltersByState enqueues a handful of jobs and verifies that
// List(state=...) only returns the matching ones.
func TestListFiltersByState(t *testing.T) {
	// Use a fast no-op handler so we can complete enqueues quickly.
	var wg sync.WaitGroup
	register := func(m *Manager) error {
		return m.Registry().RegisterGo(Definition{
			Name:        "noop-job",
			Language:    LanguageGo,
			MaxAttempts: 1,
		}, func(ctx context.Context, args []byte) error {
			defer wg.Done()
			return nil
		})
	}
	mgr, _, cleanup := testManagerSetup(t, register)
	defer cleanup()

	ctx := context.Background()
	const enqueued = 5
	wg.Add(enqueued)
	for i := 0; i < enqueued; i++ {
		if _, err := mgr.Enqueue(ctx, "noop-job", json.RawMessage(`{}`), EnqueueOpts{}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	wg.Wait()

	// Give River a beat to land the completed rows.
	time.Sleep(300 * time.Millisecond)

	// completed: should see all 5.
	res, err := mgr.List(ctx, ListFilters{State: string(rivertype.JobStateCompleted), Limit: 20})
	if err != nil {
		t.Fatalf("list completed: %v", err)
	}
	if res.Total < enqueued {
		t.Errorf("expected at least %d completed, got %d", enqueued, res.Total)
	}

	// running: should be 0 (everything finished).
	running, err := mgr.List(ctx, ListFilters{State: string(rivertype.JobStateRunning), Limit: 20})
	if err != nil {
		t.Fatalf("list running: %v", err)
	}
	if running.Total != 0 {
		t.Errorf("expected 0 running, got %d", running.Total)
	}

	// name filter: noop-job matches all
	byName, err := mgr.List(ctx, ListFilters{Name: "noop-job", Limit: 20})
	if err != nil {
		t.Fatalf("list by name: %v", err)
	}
	if byName.Total < enqueued {
		t.Errorf("expected name-filter to return >= %d, got %d", enqueued, byName.Total)
	}

	// unknown name: 0
	byUnknown, err := mgr.List(ctx, ListFilters{Name: "does-not-exist", Limit: 20})
	if err != nil {
		t.Fatalf("list unknown name: %v", err)
	}
	if byUnknown.Total != 0 {
		t.Errorf("expected 0 for unknown kind, got %d", byUnknown.Total)
	}
}

// TestRetryDiscardedJob runs a job that always fails until it discards,
// then calls Retry and expects it to be retryable again.
func TestRetryDiscardedJob(t *testing.T) {
	register := func(m *Manager) error {
		return m.Registry().RegisterGo(Definition{
			Name:        "always-fail",
			Language:    LanguageGo,
			MaxAttempts: 1, // ensures one failure -> discarded
		}, func(ctx context.Context, args []byte) error {
			return errors.New("intentional failure")
		})
	}
	mgr, _, cleanup := testManagerSetup(t, register)
	defer cleanup()

	ctx := context.Background()
	row, err := mgr.Enqueue(ctx, "always-fail", json.RawMessage(`{}`), EnqueueOpts{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Wait for it to be discarded.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		got, err := mgr.Get(ctx, row.ID)
		if err == nil && got.State == rivertype.JobStateDiscarded {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	got, err := mgr.Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != rivertype.JobStateDiscarded {
		t.Fatalf("expected discarded, got %q (errors=%v)", got.State, got.Errors)
	}

	// Retry should move it back to retryable/available.
	retried, err := mgr.Retry(ctx, row.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	switch retried.State {
	case rivertype.JobStateAvailable, rivertype.JobStateRetryable, rivertype.JobStateRunning, rivertype.JobStateScheduled:
		// ok
	default:
		t.Errorf("after retry, state = %q, expected retryable-ish", retried.State)
	}
}

// TestSummaryCountsMakeSense enqueues a mix of jobs and verifies the
// Summary buckets reflect their states.
func TestSummaryCountsMakeSense(t *testing.T) {
	var wg sync.WaitGroup
	register := func(m *Manager) error {
		if err := m.Registry().RegisterGo(Definition{
			Name: "ok-job", Language: LanguageGo, MaxAttempts: 1,
		}, func(ctx context.Context, args []byte) error {
			defer wg.Done()
			return nil
		}); err != nil {
			return err
		}
		return m.Registry().RegisterGo(Definition{
			Name: "fail-job", Language: LanguageGo, MaxAttempts: 1,
		}, func(ctx context.Context, args []byte) error {
			defer wg.Done()
			return errors.New("nope")
		})
	}
	mgr, _, cleanup := testManagerSetup(t, register)
	defer cleanup()

	ctx := context.Background()
	const okN, failN = 3, 2
	wg.Add(okN + failN)
	for i := 0; i < okN; i++ {
		if _, err := mgr.Enqueue(ctx, "ok-job", json.RawMessage(`{}`), EnqueueOpts{}); err != nil {
			t.Fatalf("enqueue ok-job %d: %v", i, err)
		}
	}
	for i := 0; i < failN; i++ {
		if _, err := mgr.Enqueue(ctx, "fail-job", json.RawMessage(`{}`), EnqueueOpts{MaxAttempts: 1}); err != nil {
			t.Fatalf("enqueue fail-job %d: %v", i, err)
		}
	}
	wg.Wait()
	time.Sleep(400 * time.Millisecond) // let River persist final states

	sum, err := mgr.Summary(ctx, 50)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.SucceededToday < okN {
		t.Errorf("succeeded_today = %d, want >= %d", sum.SucceededToday, okN)
	}
	if sum.Failed < failN {
		t.Errorf("failed = %d, want >= %d", sum.Failed, failN)
	}
	if len(sum.Recent) == 0 {
		t.Errorf("expected at least one recent row")
	}
}