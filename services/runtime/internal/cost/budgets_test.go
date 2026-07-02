// SPDX-License-Identifier: Apache-2.0

//go:build integration

// Integration tests for the cost ledger + budget store.
//
// Gated behind the `integration` build tag so `go test ./...` stays
// fast on developer machines without Postgres. Mirrors the jobs
// package convention (see jobs_test.go).
//
// To run:
//
//	docker run --rm -d --name af-cost-pg \
//	  -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:17-alpine
//	export JOBS_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable'
//	go test -tags=integration ./services/runtime/internal/cost/...
//
// Each test grabs a unique schema (cost_test_<rand>) so concurrent
// runs don't collide.

package cost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const databaseURLEnv = "JOBS_TEST_DATABASE_URL"

// testSetup creates a fresh schema, applies the minimum tables
// (tenants + cost_events + budgets), and returns Recorder/Budgets/
// Aggregate handles + cleanup.
func testSetup(t *testing.T) (*pgxpool.Pool, *Recorder, *Budgets, *Aggregate, func()) {
	t.Helper()
	dsn := os.Getenv(databaseURLEnv)
	if dsn == "" {
		t.Skipf("set %s to run cost integration tests", databaseURLEnv)
	}

	rb := make([]byte, 6)
	if _, err := rand.Read(rb); err != nil {
		t.Fatal(err)
	}
	schema := "cost_test_" + hex.EncodeToString(rb)

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
		_, err := c.Exec(ctx, fmt.Sprintf("set search_path to %q", schema))
		return err
	}
	pcfg.MaxConns = 4

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}

	// Bootstrap the minimum tables. We don't replay the full goose
	// migration here (00001 brings a lot of unrelated infra); instead
	// we create just suite_tenants + cost_events + budgets with
	// matching column types.
	mustExec(ctx, t, pool, `create extension if not exists pgcrypto`)
	mustExec(ctx, t, pool, `
        create table suite_tenants (
            id uuid primary key default gen_random_uuid(),
            slug text unique not null,
            name text not null,
            plan text not null default 'free',
            created_at timestamptz not null default now(),
            deleted_at timestamptz
        )`)
	mustExec(ctx, t, pool, `
        create table suite_api_keys (
            id uuid primary key default gen_random_uuid(),
            tenant_id uuid references suite_tenants(id) on delete cascade,
            created_at timestamptz not null default now()
        )`)
	mustExec(ctx, t, pool, `
        create table suite_cost_events (
            id uuid primary key default gen_random_uuid(),
            request_id text,
            tenant_id uuid references suite_tenants(id) on delete set null,
            api_key_id uuid references suite_api_keys(id) on delete set null,
            model text not null,
            provider text not null,
            agent text,
            reasoner text,
            prompt_tokens int not null default 0,
            completion_tokens int not null default 0,
            total_tokens int not null default 0,
            cost_usd numeric(12,6) not null default 0,
            cached boolean not null default false,
            latency_ms int not null default 0,
            occurred_at timestamptz not null default now(),
            modality text not null default 'text',
            status_code int not null default 0,
            error_code text
        )`)
	mustExec(ctx, t, pool, `
        create table suite_budgets (
            tenant_id uuid primary key references suite_tenants(id) on delete cascade,
            monthly_usd numeric(12,2) not null,
            alert_threshold_pct int not null default 80,
            current_period_start timestamptz not null default date_trunc('month', now()),
            created_at timestamptz not null default now(),
            updated_at timestamptz not null default now()
        )`)

	cleanup := func() {
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if conn, err := pgx.Connect(ctx, dsn); err == nil {
			_, _ = conn.Exec(ctx, fmt.Sprintf("drop schema if exists %q cascade", schema))
			conn.Close(ctx)
		}
	}

	return pool,
		NewRecorder(pool, nil),
		NewBudgets(pool, nil),
		NewAggregate(pool, nil),
		cleanup
}

func mustExec(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// seedTenant inserts a tenant and returns its uuid.
func seedTenant(t *testing.T, pool *pgxpool.Pool, slug, name string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		"insert into suite_tenants (slug, name) values ($1, $2) returning id::text",
		slug, name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	return id
}

// ─── Recorder + Aggregate round-trip ──────────────────────────────────────

// TestRecordEventAndAggregateSeesIt: write one cost event, then read
// it back via Aggregate.Summary + Aggregate.Events.
func TestRecordEventAndAggregateSeesIt(t *testing.T) {
	pool, rec, _, agg, cleanup := testSetup(t)
	defer cleanup()

	ctx := context.Background()
	tenant := seedTenant(t, pool, "acme", "Acme")
	occurredAt := time.Now().UTC()

	err := rec.Record(ctx, Event{
		RequestID:        "req-supportdesk-1",
		TenantID:         tenant,
		Model:            "openai/gpt-4o-mini",
		Provider:         "openai",
		Agent:            "demo.summarise",
		PromptTokens:     100,
		CompletionTokens: 50,
		CostUSD:          0.001,
		LatencyMS:        250,
		OccurredAt:       occurredAt,
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	// Aggregate.Summary should see exactly one event totalling $0.001.
	summary, err := agg.Summary(ctx, AggregateOpts{
		PeriodStart: occurredAt.Add(-time.Hour),
		PeriodEnd:   occurredAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if got := summary.PeriodTotalUSD; got < 0.0009 || got > 0.0011 {
		t.Errorf("expected period_total≈0.001, got %v", got)
	}
	if len(summary.ByModel) != 1 || summary.ByModel[0].Model != "openai/gpt-4o-mini" {
		t.Errorf("expected one by_model row for openai/gpt-4o-mini, got %+v", summary.ByModel)
	}
	if len(summary.ByAgent) != 1 || summary.ByAgent[0].Agent != "demo.summarise" {
		t.Errorf("expected one by_agent row for demo.summarise, got %+v", summary.ByAgent)
	}

	// Aggregate.Events should also see it.
	events, err := agg.Events(ctx, EventsOpts{TenantID: tenant})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if events.Total != 1 || len(events.Events) != 1 {
		t.Fatalf("expected 1 event, got total=%d len=%d", events.Total, len(events.Events))
	}
	got := events.Events[0]
	if got.Model != "openai/gpt-4o-mini" || got.Provider != "openai" {
		t.Errorf("unexpected event: %+v", got)
	}
	if got.RequestID == nil || *got.RequestID != "req-supportdesk-1" {
		t.Errorf("expected request id to round-trip, got %+v", got.RequestID)
	}
	if got.CostUSD < 0.0009 || got.CostUSD > 0.0011 {
		t.Errorf("expected cost≈0.001, got %v", got.CostUSD)
	}

	byRequest, err := agg.Events(ctx, EventsOpts{TenantID: tenant, RequestID: "req-supportdesk-1"})
	if err != nil {
		t.Fatalf("events by request: %v", err)
	}
	if byRequest.Total != 1 || len(byRequest.Events) != 1 {
		t.Fatalf("expected one event for request id, got total=%d len=%d", byRequest.Total, len(byRequest.Events))
	}

	missing, err := agg.Events(ctx, EventsOpts{TenantID: tenant, RequestID: "req-missing"})
	if err != nil {
		t.Fatalf("events by missing request: %v", err)
	}
	if missing.Total != 0 || len(missing.Events) != 0 {
		t.Fatalf("expected no events for missing request id, got total=%d len=%d", missing.Total, len(missing.Events))
	}
}

// ─── Budget round-trip ────────────────────────────────────────────────────

func TestBudgetSetAndGetRoundTrip(t *testing.T) {
	pool, _, budgets, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	tenant := seedTenant(t, pool, "acme", "Acme")

	got, err := budgets.Set(ctx, SetBudgetInput{
		TenantID:          tenant,
		MonthlyUSD:        500,
		AlertThresholdPct: 90,
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if got.MonthlyUSD != 500 || got.AlertThresholdPct != 90 {
		t.Errorf("set returned wrong fields: %+v", got)
	}
	if got.RemainingUSD != 500 {
		t.Errorf("expected RemainingUSD=500 (no spend), got %v", got.RemainingUSD)
	}

	// Get returns the same.
	loaded, err := budgets.Get(ctx, tenant)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.MonthlyUSD != 500 || loaded.AlertThresholdPct != 90 {
		t.Errorf("get returned wrong fields: %+v", loaded)
	}

	// Update: raise the cap to 1000, threshold to 70.
	updated, err := budgets.Set(ctx, SetBudgetInput{
		TenantID:          tenant,
		MonthlyUSD:        1000,
		AlertThresholdPct: 70,
	})
	if err != nil {
		t.Fatalf("set update: %v", err)
	}
	if updated.MonthlyUSD != 1000 || updated.AlertThresholdPct != 70 {
		t.Errorf("update returned wrong fields: %+v", updated)
	}
	// current_period_start should NOT have rolled forward — changing
	// the cap mid-month preserves the running spend window.
	if !updated.CurrentPeriodStart.Equal(loaded.CurrentPeriodStart) {
		t.Errorf("period_start should not change on update; got %v vs %v",
			updated.CurrentPeriodStart, loaded.CurrentPeriodStart)
	}
}

func TestBudgetGetNotFound(t *testing.T) {
	_, _, budgets, _, cleanup := testSetup(t)
	defer cleanup()
	_, err := budgets.Get(context.Background(), "00000000-0000-0000-0000-000000000099")
	if !errors.Is(err, ErrBudgetNotFound) {
		t.Errorf("expected ErrBudgetNotFound, got %v", err)
	}
}

func TestBudgetListJoinsCurrentSpend(t *testing.T) {
	pool, rec, budgets, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	tenant := seedTenant(t, pool, "acme", "Acme")
	tenant2 := seedTenant(t, pool, "beta", "Beta")
	if _, err := budgets.Set(ctx, SetBudgetInput{TenantID: tenant, MonthlyUSD: 100, AlertThresholdPct: 80}); err != nil {
		t.Fatalf("set 1: %v", err)
	}
	if _, err := budgets.Set(ctx, SetBudgetInput{TenantID: tenant2, MonthlyUSD: 200, AlertThresholdPct: 80}); err != nil {
		t.Fatalf("set 2: %v", err)
	}

	// Record some spend on tenant1 only.
	now := time.Now().UTC()
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p",
		CostUSD: 25, OccurredAt: now,
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p",
		CostUSD: 10, OccurredAt: now,
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	rows, err := budgets.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 budgets, got %d", len(rows))
	}
	byTenant := map[string]Budget{}
	for _, b := range rows {
		byTenant[b.TenantID] = b
	}
	if b := byTenant[tenant]; b.SpentThisPeriodUSD < 34.99 || b.SpentThisPeriodUSD > 35.01 {
		t.Errorf("expected spent≈35 on tenant1, got %+v", b)
	}
	if b := byTenant[tenant2]; b.SpentThisPeriodUSD != 0 {
		t.Errorf("expected spent=0 on tenant2, got %v", b.SpentThisPeriodUSD)
	}
}

// ─── HasBudget gate semantics ─────────────────────────────────────────────

func TestHasBudgetAdmitsUnderCap(t *testing.T) {
	pool, rec, budgets, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	tenant := seedTenant(t, pool, "acme", "Acme")
	if _, err := budgets.Set(ctx, SetBudgetInput{TenantID: tenant, MonthlyUSD: 100, AlertThresholdPct: 80}); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Record $10 of spend.
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p",
		CostUSD: 10, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	ok, err := budgets.HasBudget(ctx, tenant, 0.001)
	if err != nil || !ok {
		t.Errorf("expected admit (10+0.001 < 100), got ok=%v err=%v", ok, err)
	}
}

func TestHasBudgetRejectsAtCap(t *testing.T) {
	pool, rec, budgets, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	tenant := seedTenant(t, pool, "acme", "Acme")
	if _, err := budgets.Set(ctx, SetBudgetInput{TenantID: tenant, MonthlyUSD: 100, AlertThresholdPct: 80}); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Record $100 of spend.
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p",
		CostUSD: 100, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	ok, err := budgets.HasBudget(ctx, tenant, 0.001)
	if err != nil {
		t.Errorf("unexpected err: %v", err)
	}
	if ok {
		t.Errorf("expected reject at cap, got admit")
	}
}

func TestHasBudgetNoBudgetAdmits(t *testing.T) {
	pool, _, budgets, _, cleanup := testSetup(t)
	defer cleanup()
	tenant := seedTenant(t, pool, "acme", "Acme")
	// No budget set -> admit.
	ok, err := budgets.HasBudget(context.Background(), tenant, 1.0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Errorf("expected admit when no budget, got reject")
	}
}

// ─── S5 default spend ceiling (DB-backed) ─────────────────────────────────

// TestDefaultCeilingAdmitsUnderCap: a tenant with NO explicit budget
// row is admitted while month-to-date spend stays under the configured
// default ceiling. Maps to contract item C2.
func TestDefaultCeilingAdmitsUnderCap(t *testing.T) {
	pool, rec, budgets, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	budgets = budgets.WithDefaultCeilingUSD(50)
	tenant := seedTenant(t, pool, "acme", "Acme")
	// $10 of spend this month, no budget row.
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p",
		CostUSD: 10, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	ok, err := budgets.HasBudget(ctx, tenant, 0.001)
	if err != nil || !ok {
		t.Errorf("expected admit (10+0.001 < 50 default ceiling), got ok=%v err=%v", ok, err)
	}
}

// TestDefaultCeilingRejectsOverCap: the same un-budgeted tenant is
// rejected once month-to-date spend reaches the default ceiling — the
// runtime spend ceiling fires before any LiteLLM forward. Maps to
// contract item C3.
func TestDefaultCeilingRejectsOverCap(t *testing.T) {
	pool, rec, budgets, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	budgets = budgets.WithDefaultCeilingUSD(50)
	tenant := seedTenant(t, pool, "acme", "Acme")
	// $50 of spend this month, no budget row -> at the default ceiling.
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p",
		CostUSD: 50, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	ok, err := budgets.HasBudget(ctx, tenant, 0.001)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Errorf("expected reject at default ceiling (50+0.001 > 50), got admit")
	}
}

// TestExplicitBudgetOverridesDefaultCeiling: an explicit per-tenant
// budget always wins over the default ceiling — even a generous
// explicit cap admits spend that a low default ceiling would reject.
// Maps to contract item C1.
func TestExplicitBudgetOverridesDefaultCeiling(t *testing.T) {
	pool, rec, budgets, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	budgets = budgets.WithDefaultCeilingUSD(10) // low default
	tenant := seedTenant(t, pool, "acme", "Acme")
	// Explicit budget of $1000 — well above the $10 default ceiling.
	if _, err := budgets.Set(ctx, SetBudgetInput{TenantID: tenant, MonthlyUSD: 1000, AlertThresholdPct: 80}); err != nil {
		t.Fatalf("set: %v", err)
	}
	// $50 of spend — over the default ceiling, but under the explicit cap.
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p",
		CostUSD: 50, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	ok, err := budgets.HasBudget(ctx, tenant, 0.001)
	if err != nil || !ok {
		t.Errorf("expected admit (explicit $1000 cap wins over $10 default), got ok=%v err=%v", ok, err)
	}
}

// TestNoBudgetNoCeilingStillAdmits: with the default ceiling disabled
// (0), an un-budgeted tenant remains unlimited — pre-S5 back-compat.
// Maps to contract item C4.
func TestNoBudgetNoCeilingStillAdmits(t *testing.T) {
	pool, rec, budgets, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	// No WithDefaultCeilingUSD call -> ceiling 0 -> disabled.
	tenant := seedTenant(t, pool, "acme", "Acme")
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p",
		CostUSD: 100000, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	ok, err := budgets.HasBudget(ctx, tenant, 1.0)
	if err != nil || !ok {
		t.Errorf("expected admit when ceiling disabled, got ok=%v err=%v", ok, err)
	}
}

// ─── Spent over time bounds ───────────────────────────────────────────────

func TestSpentRespectsPeriodWindow(t *testing.T) {
	pool, rec, budgets, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	tenant := seedTenant(t, pool, "acme", "Acme")
	periodStart := time.Now().UTC().AddDate(0, 0, -10) // 10 days ago

	// Inside the window:
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p", CostUSD: 5,
		OccurredAt: time.Now().UTC().AddDate(0, 0, -5),
	}); err != nil {
		t.Fatalf("record in: %v", err)
	}
	// Before the window (outside):
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p", CostUSD: 100,
		OccurredAt: time.Now().UTC().AddDate(0, 0, -20),
	}); err != nil {
		t.Fatalf("record out: %v", err)
	}
	// More than 1 month after start (outside):
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p", CostUSD: 200,
		OccurredAt: periodStart.AddDate(0, 1, 5),
	}); err != nil {
		t.Fatalf("record after: %v", err)
	}

	spent, err := budgets.Spent(ctx, tenant, periodStart)
	if err != nil {
		t.Fatalf("spent: %v", err)
	}
	if spent < 4.99 || spent > 5.01 {
		t.Errorf("expected spent=5 (only the in-window event), got %v", spent)
	}
}

// ─── Tenant isolation ─────────────────────────────────────────────────────

// TestTenantScopingInAggregate: tenant A's events do not appear in
// tenant B's summary. Both tenants exist; both have events.
func TestTenantScopingInAggregate(t *testing.T) {
	pool, rec, _, agg, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	a := seedTenant(t, pool, "acme", "Acme")
	b := seedTenant(t, pool, "beta", "Beta")
	occurred := time.Now().UTC()

	if err := rec.Record(ctx, Event{TenantID: a, Model: "m", Provider: "p", CostUSD: 7, OccurredAt: occurred}); err != nil {
		t.Fatalf("record a: %v", err)
	}
	if err := rec.Record(ctx, Event{TenantID: b, Model: "m", Provider: "p", CostUSD: 11, OccurredAt: occurred}); err != nil {
		t.Fatalf("record b: %v", err)
	}

	// Scope to tenant A only — sees $7, not $18.
	sA, err := agg.Summary(ctx, AggregateOpts{
		TenantID:    a,
		PeriodStart: occurred.Add(-time.Hour),
		PeriodEnd:   occurred.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("summary A: %v", err)
	}
	if sA.PeriodTotalUSD < 6.99 || sA.PeriodTotalUSD > 7.01 {
		t.Errorf("expected period_total=7 for tenant A, got %v", sA.PeriodTotalUSD)
	}

	// Scope to tenant B only — sees $11.
	sB, err := agg.Summary(ctx, AggregateOpts{
		TenantID:    b,
		PeriodStart: occurred.Add(-time.Hour),
		PeriodEnd:   occurred.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("summary B: %v", err)
	}
	if sB.PeriodTotalUSD < 10.99 || sB.PeriodTotalUSD > 11.01 {
		t.Errorf("expected period_total=11 for tenant B, got %v", sB.PeriodTotalUSD)
	}

	// Cross-tenant view (no tenant scope) sees $18 and two by_tenant rows.
	sAll, err := agg.Summary(ctx, AggregateOpts{
		PeriodStart: occurred.Add(-time.Hour),
		PeriodEnd:   occurred.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("summary all: %v", err)
	}
	if sAll.PeriodTotalUSD < 17.99 || sAll.PeriodTotalUSD > 18.01 {
		t.Errorf("expected period_total=18 cross-tenant, got %v", sAll.PeriodTotalUSD)
	}
	if len(sAll.ByTenant) != 2 {
		t.Errorf("expected 2 by_tenant rows, got %d", len(sAll.ByTenant))
	}
}

// TestEventsTenantFilter: tenant filter on the events list excludes
// other tenants' events.
func TestEventsTenantFilter(t *testing.T) {
	pool, rec, _, agg, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	a := seedTenant(t, pool, "acme", "Acme")
	b := seedTenant(t, pool, "beta", "Beta")

	for i := 0; i < 3; i++ {
		_ = rec.Record(ctx, Event{TenantID: a, Model: "m", Provider: "p", CostUSD: 1})
	}
	for i := 0; i < 5; i++ {
		_ = rec.Record(ctx, Event{TenantID: b, Model: "m", Provider: "p", CostUSD: 1})
	}

	pageA, _ := agg.Events(ctx, EventsOpts{TenantID: a})
	if pageA.Total != 3 {
		t.Errorf("expected 3 events for tenant A, got %d", pageA.Total)
	}
	pageB, _ := agg.Events(ctx, EventsOpts{TenantID: b})
	if pageB.Total != 5 {
		t.Errorf("expected 5 events for tenant B, got %d", pageB.Total)
	}
}

// ─── Summary previous period + budget ─────────────────────────────────────

func TestSummaryPreviousPeriodAndBudget(t *testing.T) {
	pool, rec, budgets, agg, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	tenant := seedTenant(t, pool, "acme", "Acme")

	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	previousStart := periodStart.AddDate(0, -1, 0)

	// Set a budget — should surface in summary.BudgetUSD.
	if _, err := budgets.Set(ctx, SetBudgetInput{
		TenantID: tenant, MonthlyUSD: 500, AlertThresholdPct: 80,
	}); err != nil {
		t.Fatalf("set: %v", err)
	}

	// $10 in current period, $40 in previous.
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p",
		CostUSD: 10, OccurredAt: periodStart.Add(time.Hour),
	}); err != nil {
		t.Fatalf("record current: %v", err)
	}
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p",
		CostUSD: 40, OccurredAt: previousStart.Add(time.Hour),
	}); err != nil {
		t.Fatalf("record previous: %v", err)
	}

	s, err := agg.Summary(ctx, AggregateOpts{TenantID: tenant})
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if s.PeriodTotalUSD < 9.99 || s.PeriodTotalUSD > 10.01 {
		t.Errorf("expected period_total≈10, got %v", s.PeriodTotalUSD)
	}
	if s.PreviousTotalUSD < 39.99 || s.PreviousTotalUSD > 40.01 {
		t.Errorf("expected previous_total≈40, got %v", s.PreviousTotalUSD)
	}
	if s.BudgetUSD == nil || *s.BudgetUSD != 500 {
		t.Errorf("expected BudgetUSD=500, got %v", s.BudgetUSD)
	}
}

// TestSummaryByDayBuckets ensures rows are grouped by UTC day.
func TestSummaryByDayBuckets(t *testing.T) {
	pool, rec, _, agg, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	tenant := seedTenant(t, pool, "acme", "Acme")

	day1 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 6, 2, 6, 0, 0, 0, time.UTC)
	// Two events on day1, one on day2.
	for _, ts := range []time.Time{day1, day1.Add(2 * time.Hour), day2} {
		if err := rec.Record(ctx, Event{
			TenantID: tenant, Model: "m", Provider: "p",
			CostUSD: 1, OccurredAt: ts,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	s, err := agg.Summary(ctx, AggregateOpts{
		PeriodStart: day1.Add(-time.Hour),
		PeriodEnd:   day2.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if len(s.ByDay) != 2 {
		t.Fatalf("expected 2 by_day rows, got %d (%+v)", len(s.ByDay), s.ByDay)
	}
	byDate := map[string]float64{}
	for _, d := range s.ByDay {
		byDate[d.Date] = d.CostUSD
	}
	if got := byDate["2026-06-01"]; got < 1.99 || got > 2.01 {
		t.Errorf("expected day1=2, got %v", got)
	}
	if got := byDate["2026-06-02"]; got < 0.99 || got > 1.01 {
		t.Errorf("expected day2=1, got %v", got)
	}
}

// TestSummaryForecastFormulaMatchesExpected: a fixed period with
// known elapsed fraction should yield the expected forecast.
//
// We use a period in the past so we control "now" via the period
// end being in the past — that path returns the observed total
// (closed period). For mid-period extrapolation we rely on the
// unit tests of forecastExtrapolate.
func TestSummaryForecastClosedPeriod(t *testing.T) {
	pool, rec, _, agg, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()
	tenant := seedTenant(t, pool, "acme", "Acme")

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p",
		CostUSD: 25.0, OccurredAt: periodStart.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	s, err := agg.Summary(ctx, AggregateOpts{
		TenantID:    tenant,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	})
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	// Period ended in the past -> forecast equals total.
	if s.ForecastUSD < 24.99 || s.ForecastUSD > 25.01 {
		t.Errorf("expected forecast≈25 (closed period), got %v", s.ForecastUSD)
	}
}

// ─── CostForExecution best-effort join ────────────────────────────────────

func TestCostForExecutionApproximateJoin(t *testing.T) {
	pool, rec, _, agg, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	// CostForExecution joins to suite_gateway_requests, which the
	// minimal test schema doesn't include. Create it just enough for
	// the join to work.
	mustExec(ctx, t, pool, `
        create table suite_gateway_requests (
            id uuid primary key default gen_random_uuid(),
            tenant_id uuid,
            af_execution_id text,
            created_at timestamptz not null default now()
        )`)

	tenant := seedTenant(t, pool, "acme", "Acme")
	execID := "exec-abc"
	t0 := time.Now().UTC()

	// Insert a gateway-request row carrying the execution id.
	if _, err := pool.Exec(ctx,
		"insert into suite_gateway_requests (tenant_id, af_execution_id, created_at) values ($1,$2,$3)",
		tenant, execID, t0,
	); err != nil {
		t.Fatalf("insert gw request: %v", err)
	}
	// Insert a cost event within the join window.
	if err := rec.Record(ctx, Event{
		TenantID: tenant, Model: "m", Provider: "p",
		CostUSD: 0.42, OccurredAt: t0.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	cost, ok, err := agg.CostForExecution(ctx, execID)
	if err != nil {
		t.Fatalf("cost for execution: %v", err)
	}
	if !ok {
		t.Fatalf("expected match, got ok=false")
	}
	if cost < 0.419 || cost > 0.421 {
		t.Errorf("expected cost≈0.42, got %v", cost)
	}

	// Unknown execution id -> (0, false, nil).
	cost2, ok2, err := agg.CostForExecution(ctx, "missing-exec")
	if err != nil {
		t.Fatalf("missing: %v", err)
	}
	if ok2 || cost2 != 0 {
		t.Errorf("expected miss=(0,false), got (%v,%v)", cost2, ok2)
	}
}
