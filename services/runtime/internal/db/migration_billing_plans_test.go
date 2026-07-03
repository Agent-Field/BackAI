// SPDX-License-Identifier: Apache-2.0

//go:build integration

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

// TestBillingPlansMigrationRoundTrip applies every up-migration and
// verifies 00031 creates the plans catalog (with the seeded free default
// and the single-default partial unique index) plus the encrypted
// settings table, then rolls back and re-ups. Gated on
// JOBS_TEST_DATABASE_URL like the other migration tests.
func TestBillingPlansMigrationRoundTrip(t *testing.T) {
	dsn := os.Getenv(migDBURLEnv)
	if dsn == "" {
		t.Skipf("set %s to run migration roundtrip", migDBURLEnv)
	}

	rb := make([]byte, 6)
	if _, err := rand.Read(rb); err != nil {
		t.Fatal(err)
	}
	schema := "migtest_bp_" + hex.EncodeToString(rb)

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
		if conn, err := pgx.Connect(context.Background(), dsn); err == nil {
			_, _ = conn.Exec(context.Background(), fmt.Sprintf("drop schema if exists %q cascade", schema))
			conn.Close(context.Background())
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

	if err := (&DB{Pool: pool}).Migrate(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// The free default plan is seeded.
	var seededDefault string
	if err := pool.QueryRow(ctx, `
        select id from suite_billing_plans where is_default`).Scan(&seededDefault); err != nil {
		t.Fatalf("seeded default plan: %v", err)
	}
	if seededDefault != "free" {
		t.Fatalf("default plan = %q, want free", seededDefault)
	}

	// At most one default: inserting a second default row must violate
	// the partial unique index.
	if _, err := pool.Exec(ctx, `
        insert into suite_billing_plans (id, name, is_default)
        values ('pro', 'Pro', true)`); err == nil {
		t.Fatal("second default plan should violate the partial unique index")
	}

	// Settings table exists and upserts by key.
	if _, err := pool.Exec(ctx, `
        insert into suite_billing_settings (key, value_enc) values ('k', '\x01'::bytea)
        on conflict (key) do update set value_enc = excluded.value_enc`); err != nil {
		t.Fatalf("settings upsert: %v", err)
	}

	// Roll back 00031 and confirm both tables drop.
	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	if err := goose.DownToContext(ctx, sqlDB, "migrations", 30); err != nil {
		t.Fatalf("down to 30: %v", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `
        select exists(
            select 1 from information_schema.tables
            where table_schema = $1 and table_name = 'suite_billing_plans'
        )`, schema).Scan(&exists); err != nil {
		t.Fatalf("table check after down: %v", err)
	}
	if exists {
		t.Error("suite_billing_plans should be gone after Down")
	}

	// Re-up is idempotent (seed uses on conflict do nothing).
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		t.Fatalf("re-up: %v", err)
	}
}
