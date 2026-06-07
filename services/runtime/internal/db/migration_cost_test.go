//go:build integration

// Integration test for the 00005_cost migration: Up/Down round-trip.
//
// Gated by the `integration` build tag so `go test ./...` stays fast.
// Reuses JOBS_TEST_DATABASE_URL (same convention as jobs + cost
// packages).
//
// To run:
//
//	export JOBS_TEST_DATABASE_URL='postgres://afstack:afstack@localhost:5432/afstack?sslmode=disable'
//	go test -tags=integration -run TestCostMigrationRoundTrip ./services/runtime/internal/db/...

package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const migDBURLEnv = "JOBS_TEST_DATABASE_URL"

// TestCostMigrationRoundTrip applies every up-migration and then rolls
// back to verify the 00005_cost Down block works. Uses a fresh schema
// so it never clobbers the dev DB.
func TestCostMigrationRoundTrip(t *testing.T) {
	dsn := os.Getenv(migDBURLEnv)
	if dsn == "" {
		t.Skipf("set %s to run migration roundtrip", migDBURLEnv)
	}

	rb := make([]byte, 6)
	if _, err := rand.Read(rb); err != nil {
		t.Fatal(err)
	}
	schema := "migtest_" + hex.EncodeToString(rb)

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

	defer func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if conn, err := pgx.Connect(ctx, dsn); err == nil {
			_, _ = conn.Exec(ctx, fmt.Sprintf("drop schema if exists %q cascade", schema))
			conn.Close(ctx)
		}
	}()

	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pcfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, fmt.Sprintf("set search_path to %q", schema))
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	d := &DB{Pool: pool}
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// Verify suite_cost_events + suite_budgets exist.
	var have int
	err = pool.QueryRow(ctx, `
        select count(*) from information_schema.tables
         where table_schema = $1
           and table_name in ('suite_cost_events','suite_budgets')
    `, schema).Scan(&have)
	if err != nil {
		t.Fatalf("table count: %v", err)
	}
	if have != 2 {
		t.Errorf("expected both cost tables after Up, found %d", have)
	}

	// Roll back the 00005 migration only and verify both tables drop.
	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	if err := goose.DownToContext(ctx, sqlDB, "migrations", 4); err != nil {
		t.Fatalf("down to 4: %v", err)
	}

	err = pool.QueryRow(ctx, `
        select count(*) from information_schema.tables
         where table_schema = $1
           and table_name in ('suite_cost_events','suite_budgets')
    `, schema).Scan(&have)
	if err != nil {
		t.Fatalf("table count after down: %v", err)
	}
	if have != 0 {
		t.Errorf("expected cost tables gone after Down, still found %d", have)
	}

	// Re-apply Up to confirm idempotent re-application works.
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	err = pool.QueryRow(ctx, `
        select count(*) from information_schema.tables
         where table_schema = $1
           and table_name in ('suite_cost_events','suite_budgets')
    `, schema).Scan(&have)
	if err != nil {
		t.Fatalf("table count after re-up: %v", err)
	}
	if have != 2 {
		t.Errorf("expected both cost tables after re-Up, found %d", have)
	}
}
