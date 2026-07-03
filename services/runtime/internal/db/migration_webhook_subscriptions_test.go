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

// TestWebhookSubscriptionsMigrationRoundTrip applies every up-migration and
// verifies 00030 creates suite_webhook_subscriptions with its defaults, then
// rolls the migration back and confirms the table drops. Gated on
// JOBS_TEST_DATABASE_URL (same convention as the other migration tests) so
// it skips cleanly in CI (which runs no DB).
func TestWebhookSubscriptionsMigrationRoundTrip(t *testing.T) {
	dsn := os.Getenv(migDBURLEnv)
	if dsn == "" {
		t.Skipf("set %s to run migration roundtrip", migDBURLEnv)
	}

	rb := make([]byte, 6)
	if _, err := rand.Read(rb); err != nil {
		t.Fatal(err)
	}
	schema := "migtest_ws_" + hex.EncodeToString(rb)

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

	// Table exists after Up.
	var exists bool
	if err := pool.QueryRow(ctx, `
        select exists(
            select 1 from information_schema.tables
            where table_schema = $1 and table_name = 'suite_webhook_subscriptions'
        )`, schema).Scan(&exists); err != nil {
		t.Fatalf("table check: %v", err)
	}
	if !exists {
		t.Fatal("suite_webhook_subscriptions missing after Up")
	}

	// Column defaults behave: events -> '{}', is_active -> true. Insert a
	// tenant (FK target) + a subscription with only the required columns.
	var tenantID string
	if err := pool.QueryRow(ctx, `
        insert into suite_tenants (slug, name) values ('nwtest', 'NW Test')
        returning id`).Scan(&tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	var (
		gotEvents   []string
		gotActive   bool
		gotSecret   string
	)
	if err := pool.QueryRow(ctx, `
        insert into suite_webhook_subscriptions (tenant_id, url, secret)
        values ($1, 'https://sub.example/hook', 'whsec_x')
        returning events, is_active, secret`, tenantID).
		Scan(&gotEvents, &gotActive, &gotSecret); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	if len(gotEvents) != 0 {
		t.Errorf("events default = %v, want empty", gotEvents)
	}
	if !gotActive {
		t.Errorf("is_active default = false, want true")
	}
	if gotSecret != "whsec_x" {
		t.Errorf("secret = %q", gotSecret)
	}

	// event-array membership works both ways (empty = all; specific match).
	var matches int
	if err := pool.QueryRow(ctx, `
        select count(*) from suite_webhook_subscriptions
        where is_active and (cardinality(events) = 0 or 'verdict.ready' = any(events))
    `).Scan(&matches); err != nil {
		t.Fatalf("event match query: %v", err)
	}
	if matches != 1 {
		t.Errorf("empty-events subscription should match any event; got %d", matches)
	}

	// Roll back 00030 and confirm the table drops.
	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	if err := goose.DownToContext(ctx, sqlDB, "migrations", 29); err != nil {
		t.Fatalf("down to 29: %v", err)
	}
	if err := pool.QueryRow(ctx, `
        select exists(
            select 1 from information_schema.tables
            where table_schema = $1 and table_name = 'suite_webhook_subscriptions'
        )`, schema).Scan(&exists); err != nil {
		t.Fatalf("table check after down: %v", err)
	}
	if exists {
		t.Error("suite_webhook_subscriptions should be gone after Down")
	}

	// Re-up is idempotent.
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		t.Fatalf("re-up: %v", err)
	}
}
