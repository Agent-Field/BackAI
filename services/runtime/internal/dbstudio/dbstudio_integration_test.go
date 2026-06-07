//go:build integration

// Integration tests for the dbstudio package.
//
// Gated behind the `integration` build tag so `go test ./...` stays
// fast on developer machines without Postgres. Mirrors the cost +
// jobs package conventions (see services/runtime/internal/cost/
// budgets_test.go).
//
// To run:
//
//	docker run --rm -d --name af-pg \
//	  -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:17-alpine
//	export JOBS_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable'
//	go test -tags=integration ./services/runtime/internal/dbstudio/...
//
// Each test creates its own unique schema (dbstudio_test_<rand>) so
// concurrent runs don't collide.
package dbstudio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const databaseURLEnv = "JOBS_TEST_DATABASE_URL"

// testSetup builds a private schema, applies a tiny test table with
// PK + index + RLS policy, and returns a Studio wired against a pool
// pinned to that schema via AfterConnect.
//
// The dbstudio package queries information_schema and pg_catalog
// directly, so we don't need to seed migrations — just a real
// relation the queries can see.
func testSetup(t *testing.T) (*pgxpool.Pool, *Studio, string, func()) {
	t.Helper()
	dsn := os.Getenv(databaseURLEnv)
	if dsn == "" {
		t.Skipf("set %s to run dbstudio integration tests", databaseURLEnv)
	}

	rb := make([]byte, 6)
	if _, err := rand.Read(rb); err != nil {
		t.Fatal(err)
	}
	schema := "dbstudio_test_" + hex.EncodeToString(rb)

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
	pcfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, fmt.Sprintf("set search_path to %q, public", schema))
		return err
	}
	pcfg.MaxConns = 4

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}

	// Seed: a single test table with a PK, a non-PK index, RLS
	// enabled, and one policy. Every detail surfaces in our
	// Table()/ListTables() assertions.
	const seed = `
		create table widgets (
			id    serial primary key,
			name  text not null,
			label text
		);
		create index widgets_label_idx on widgets (label);
		alter table widgets enable row level security;
		create policy widgets_select on widgets
			for select using (true);
		insert into widgets (name, label) values
			('alpha', 'a'),
			('beta',  'b'),
			('gamma', 'c'),
			('delta', null);
	`
	if _, err := pool.Exec(ctx, seed); err != nil {
		pool.Close()
		t.Fatalf("seed: %v", err)
	}

	studio := New(pool)
	cleanup := func() {
		// Drop the schema so the next run doesn't pollute (and the
		// outer DB stays cheap).
		dropCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		drop, err := pgx.Connect(dropCtx, dsn)
		if err == nil {
			_, _ = drop.Exec(dropCtx, fmt.Sprintf("drop schema %q cascade", schema))
			drop.Close(dropCtx)
		}
		pool.Close()
	}
	return pool, studio, schema, cleanup
}

func TestIntegrationListTables(t *testing.T) {
	_, studio, schema, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	tables, err := studio.ListTables(ctx)
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	found := false
	for _, tbl := range tables {
		if tbl.Schema == schema && tbl.Name == "widgets" {
			found = true
			if tbl.Kind != "table" {
				t.Errorf("widgets kind = %q, want table", tbl.Kind)
			}
			if !tbl.HasRLS {
				t.Errorf("widgets HasRLS = false; alter table enable rls failed?")
			}
			// EstimatedRows can be 0 immediately after insert (PG hasn't
			// run ANALYZE yet). Don't assert exact value.
		}
	}
	if !found {
		t.Errorf("widgets not found in ListTables (schema=%q); got: %+v", schema, tables)
	}
}

func TestIntegrationTableDetail(t *testing.T) {
	_, studio, schema, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	detail, err := studio.Table(ctx, schema, "widgets")
	if err != nil {
		t.Fatalf("Table: %v", err)
	}
	if detail.Kind != "table" {
		t.Errorf("Kind = %q, want table", detail.Kind)
	}
	if !detail.RLSEnabled {
		t.Errorf("RLSEnabled = false; seed should have enabled it")
	}

	// Columns: id (pk), name (not null), label (nullable)
	cols := map[string]DBColumn{}
	for _, c := range detail.Columns {
		cols[c.Name] = c
	}
	if len(cols) != 3 {
		t.Errorf("want 3 columns, got %d: %+v", len(cols), detail.Columns)
	}
	if id, ok := cols["id"]; ok {
		if !id.IsPrimaryKey {
			t.Errorf("id.IsPrimaryKey = false")
		}
	} else {
		t.Errorf("missing id column")
	}
	if label, ok := cols["label"]; ok {
		if !label.IsNullable {
			t.Errorf("label.IsNullable = false; should be nullable")
		}
	} else {
		t.Errorf("missing label column")
	}

	// Indexes: at least the PK + the named one.
	idxNames := map[string]DBIndex{}
	for _, ix := range detail.Indexes {
		idxNames[ix.Name] = ix
	}
	if _, ok := idxNames["widgets_label_idx"]; !ok {
		t.Errorf("widgets_label_idx not found; got: %+v", detail.Indexes)
	}
	// There should be one primary-key index.
	primaryCount := 0
	for _, ix := range detail.Indexes {
		if ix.IsPrimary {
			primaryCount++
		}
	}
	if primaryCount != 1 {
		t.Errorf("want 1 primary index, got %d", primaryCount)
	}

	// Policy: widgets_select with cmd SELECT.
	if len(detail.Policies) != 1 {
		t.Errorf("want 1 policy, got %d: %+v", len(detail.Policies), detail.Policies)
	} else {
		p := detail.Policies[0]
		if p.Name != "widgets_select" {
			t.Errorf("policy.Name = %q, want widgets_select", p.Name)
		}
		if p.Cmd != "SELECT" {
			t.Errorf("policy.Cmd = %q, want SELECT", p.Cmd)
		}
		if !p.Permissive {
			t.Errorf("policy.Permissive = false; create policy default is permissive")
		}
	}
}

func TestIntegrationRowsPagination(t *testing.T) {
	_, studio, schema, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	// limit=2, offset=0 -> 2 rows
	res, err := studio.Rows(ctx, schema, "widgets", 2, 0)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if res.RowCount != 2 {
		t.Errorf("limit=2 RowCount = %d, want 2", res.RowCount)
	}
	if len(res.Rows) != 2 {
		t.Errorf("limit=2 len(Rows) = %d, want 2", len(res.Rows))
	}
	// Columns: id, name, label
	if len(res.Columns) != 3 {
		t.Errorf("Columns = %v, want 3", res.Columns)
	}

	// offset past end -> 0 rows
	res2, err := studio.Rows(ctx, schema, "widgets", 10, 1000)
	if err != nil {
		t.Fatalf("Rows past end: %v", err)
	}
	if res2.RowCount != 0 {
		t.Errorf("offset past end RowCount = %d, want 0", res2.RowCount)
	}
}

func TestIntegrationRowsRejectsInjection(t *testing.T) {
	_, studio, schema, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	// Schema valid, table is an injection attempt.
	_, err := studio.Rows(ctx, schema, `widgets"; drop table widgets --`, 10, 0)
	if !errors.Is(err, ErrInvalidIdentifier) {
		t.Errorf("Rows with injection table = %v, want ErrInvalidIdentifier", err)
	}
	// Schema is the injection attempt.
	_, err = studio.Rows(ctx, `public"; drop --`, "widgets", 10, 0)
	if !errors.Is(err, ErrInvalidIdentifier) {
		t.Errorf("Rows with injection schema = %v, want ErrInvalidIdentifier", err)
	}
}

func TestIntegrationRunSQLSelectReadOnly(t *testing.T) {
	_, studio, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	res, err := studio.RunSQL(ctx, "select 1::int as n", true, 0)
	if err != nil {
		t.Fatalf("RunSQL select: %v", err)
	}
	if len(res.Columns) != 1 || res.Columns[0] != "n" {
		t.Errorf("Columns = %v, want [n]", res.Columns)
	}
	if res.RowCount != 1 {
		t.Errorf("RowCount = %d, want 1", res.RowCount)
	}
	if len(res.Rows) != 1 || len(res.Rows[0]) != 1 {
		t.Fatalf("Rows shape = %+v, want [[1]]", res.Rows)
	}
	// The scanned value should be an int32 (PG int4); compare via fmt to
	// avoid pinning the exact Go type.
	if fmt.Sprintf("%v", res.Rows[0][0]) != "1" {
		t.Errorf("Rows[0][0] = %v, want 1", res.Rows[0][0])
	}
}

func TestIntegrationRunSQLDropReadOnlyRejected(t *testing.T) {
	_, studio, schema, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	stmt := fmt.Sprintf(`drop table %q.widgets`, schema)
	_, err := studio.RunSQL(ctx, stmt, true, 0)
	if !errors.Is(err, ErrReadOnlyViolation) {
		t.Errorf("read-only DROP = %v, want ErrReadOnlyViolation", err)
	}
}

func TestIntegrationRunSQLInsertReadOnlyRejected(t *testing.T) {
	_, studio, schema, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	stmt := fmt.Sprintf(
		`insert into %q.widgets (name, label) values ('x','y')`, schema)
	_, err := studio.RunSQL(ctx, stmt, true, 0)
	if !errors.Is(err, ErrReadOnlyViolation) {
		t.Errorf("read-only INSERT = %v, want ErrReadOnlyViolation", err)
	}
}

func TestIntegrationRunSQLLimitTruncates(t *testing.T) {
	_, studio, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	// generate_series gives 100 rows; limit=10 -> Truncated=true.
	res, err := studio.RunSQL(ctx, "select generate_series(1,100) as n", true, 10)
	if err != nil {
		t.Fatalf("RunSQL series: %v", err)
	}
	if res.RowCount != 10 {
		t.Errorf("RowCount = %d, want 10 (capped at limit)", res.RowCount)
	}
	if !res.Truncated {
		t.Errorf("Truncated = false, want true")
	}
}
