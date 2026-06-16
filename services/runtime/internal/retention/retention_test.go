// SPDX-License-Identifier: Apache-2.0

package retention

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestRegistryRunDeletesRowsInBatches(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()

	ctx := context.Background()
	mustExec(t, db, `create table suite_provider_health_log (id serial primary key, observed_at timestamptz not null)`)
	for i := 0; i < 5; i++ {
		mustExec(t, db, `insert into suite_provider_health_log (observed_at) values (now() - interval '31 days')`)
	}
	for i := 0; i < 2; i++ {
		mustExec(t, db, `insert into suite_provider_health_log (observed_at) values (now())`)
	}

	reg := NewRegistry()
	reg.Register(Policy{Table: "suite_provider_health_log", RetainDays: 30, OrderColumn: "observed_at", BatchSize: 2})
	report, err := reg.Run(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if report.RowsDeleted != 5 {
		t.Fatalf("RowsDeleted = %d, want 5", report.RowsDeleted)
	}
	report, err = reg.Run(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if report.RowsDeleted != 0 {
		t.Fatalf("second RowsDeleted = %d, want 0", report.RowsDeleted)
	}
}

func TestRegistryRunHonorsCancelledContext(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	mustExec(t, db, `create table suite_provider_health_log (id serial primary key, observed_at timestamptz not null)`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reg := NewRegistry()
	reg.Register(Policy{Table: "suite_provider_health_log", RetainDays: 30, OrderColumn: "observed_at", BatchSize: 1})
	if _, err := reg.Run(ctx, db); err == nil {
		t.Fatal("Run returned nil err, want cancellation")
	}
}

func testDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("AF_STACK_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	schema := "retention_test_" + randHex(t)
	bootstrap, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Exec(ctx, fmt.Sprintf(`create schema %q`, schema)); err != nil {
		bootstrap.Close()
		t.Fatal(err)
	}
	bootstrap.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf(`set search_path to %q`, schema))
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`drop schema if exists %q cascade`, schema))
		_ = sqlDB.Close()
	}
	mustExec(t, sqlDB, fmt.Sprintf(`set search_path to %q`, schema))
	return sqlDB, cleanup
}

func randHex(t *testing.T) string {
	t.Helper()
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), q); err != nil {
		t.Fatal(err)
	}
}
