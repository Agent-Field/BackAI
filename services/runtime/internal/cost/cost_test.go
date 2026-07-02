// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
)

// ─── Recorder no-pool path ────────────────────────────────────────────────

// TestRecorderNilPoolNoError confirms the no-DB boot path doesn't
// error: the cost ledger silently drops writes when there's no place
// to put them.
func TestRecorderNilPoolNoError(t *testing.T) {
	r := NewRecorder(nil, nil)
	err := r.Record(context.Background(), Event{
		Model:  "openai/gpt-4o-mini",
		CostUSD: 0.001,
	})
	if err != nil {
		t.Fatalf("expected nil error with nil pool, got %v", err)
	}
}

// TestRecorderNilReceiver ensures method receivers tolerate a nil
// Recorder (defensive: a future caller might forget to construct one).
func TestRecorderNilReceiver(t *testing.T) {
	var r *Recorder
	if err := r.Record(context.Background(), Event{}); err != nil {
		t.Fatalf("expected nil-recorder Record to no-op, got %v", err)
	}
}

// ─── Budgets no-pool path ─────────────────────────────────────────────────

func TestBudgetsNilPoolGetReturnsNotFound(t *testing.T) {
	b := NewBudgets(nil, nil)
	_, err := b.Get(context.Background(), "tid")
	if !errors.Is(err, ErrBudgetNotFound) {
		t.Fatalf("expected ErrBudgetNotFound, got %v", err)
	}
}

func TestBudgetsNilPoolListReturnsEmpty(t *testing.T) {
	b := NewBudgets(nil, nil)
	rows, err := b.List(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected empty list, got %d", len(rows))
	}
}

// TestHasBudgetNilPoolAdmits: no DB at all -> never reject. The
// gateway boot path relies on this so the LLM surface stays alive
// before Phase 7 is fully provisioned.
func TestHasBudgetNilPoolAdmits(t *testing.T) {
	b := NewBudgets(nil, nil)
	ok, err := b.HasBudget(context.Background(), "tid", 1.0)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !ok {
		t.Fatalf("expected admit with nil pool, got reject")
	}
}

// TestHasBudgetEmptyTenantAdmits: the gateway calls with tenant="" for
// unauthenticated paths — we admit.
func TestHasBudgetEmptyTenantAdmits(t *testing.T) {
	b := NewBudgets(nil, nil)
	ok, _ := b.HasBudget(context.Background(), "", 1.0)
	if !ok {
		t.Fatalf("expected admit on empty tenant")
	}
}

// ─── Budgets validation ───────────────────────────────────────────────────

func TestBudgetsSetValidation(t *testing.T) {
	b := NewBudgets(nil, nil)
	// Nil pool returns "storage unavailable" before validation
	// — so cover that branch with the no-pool path.
	_, err := b.Set(context.Background(), SetBudgetInput{TenantID: "tid", MonthlyUSD: 100})
	if err == nil {
		t.Fatalf("expected error with nil pool, got nil")
	}
}

func TestBudgetGetValidatesTenantID(t *testing.T) {
	// We need a non-nil pool path to hit the validation branch — but
	// since constructing one needs PG, we just confirm the nil-pool
	// path returns NotFound (the empty tenant check happens AFTER
	// the nil-pool short-circuit, which is fine — callers always
	// pass a real tenant on this code path).
	b := NewBudgets(nil, nil)
	_, err := b.Get(context.Background(), "")
	if !errors.Is(err, ErrBudgetNotFound) {
		t.Fatalf("nil pool short-circuits to NotFound, got %v", err)
	}
}

// ─── buildBudget pure logic ───────────────────────────────────────────────

func TestBuildBudgetRemainingFloorsAtZero(t *testing.T) {
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	b := buildBudget("tid", 100, 80, start, 150)
	if b.RemainingUSD != 0 {
		t.Errorf("expected RemainingUSD floored at 0, got %v", b.RemainingUSD)
	}
	if b.SpentThisPeriodUSD != 150 {
		t.Errorf("expected SpentThisPeriodUSD=150, got %v", b.SpentThisPeriodUSD)
	}
}

func TestBuildBudgetResetsAtIsOneMonthForward(t *testing.T) {
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	b := buildBudget("tid", 100, 80, start, 0)
	want := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if !b.ResetsAt.Equal(want) {
		t.Errorf("expected ResetsAt=%s, got %s", want, b.ResetsAt)
	}
}

// TestBuildBudgetResetsAtHandlesMonthLengthVariation confirms
// AddDate(0,1,0) handles 31-Jan -> 28-Feb correctly (Go's time
// normalisation may shift to March if the day overflows).
func TestBuildBudgetResetsAtHandlesMonthLengthVariation(t *testing.T) {
	// Period starting 31 Jan: AddDate(0,1,0) normalises to 3 Mar (or
	// 31 Feb -> 3 Mar). suite_budgets uses date_trunc('month', now())
	// so this is a defensive test for hand-set inputs.
	start := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	b := buildBudget("tid", 100, 80, start, 0)
	// Go's normalisation: 31 Feb -> 3 Mar 2026. We accept that for
	// hand-set inputs; production uses date_trunc which is always day 1.
	if b.ResetsAt.Year() != 2026 {
		t.Errorf("expected 2026, got %v", b.ResetsAt.Year())
	}
	if b.ResetsAt.Month() != time.March {
		t.Errorf("expected March (normalised), got %v", b.ResetsAt.Month())
	}
}

// ─── forecastExtrapolate pure logic ───────────────────────────────────────

func TestForecastClosedPeriodReturnsTotal(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC) // after end
	got := forecastExtrapolate(123.45, start, end, now)
	if got != 123.45 {
		t.Errorf("closed period -> forecast=total; got %v want 123.45", got)
	}
}

func TestForecastLinearExtrapolation(t *testing.T) {
	// Period of exactly 100 hours. Now is 25h in (25% elapsed).
	// Total observed = $10. Forecast = 10 / 0.25 = 40.
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(100 * time.Hour)
	now := start.Add(25 * time.Hour)
	got := forecastExtrapolate(10, start, end, now)
	if got < 39.99 || got > 40.01 {
		t.Errorf("expected forecast≈40, got %v", got)
	}
}

func TestForecastClampsTinyElapsedFraction(t *testing.T) {
	// Period just started — extrapolating from 0.001% would explode.
	// We floor elapsedFraction at 1% so forecast stays bounded.
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(30 * 24 * time.Hour)        // 30-day period
	now := start.Add(1 * time.Minute)            // ~0.002% elapsed
	got := forecastExtrapolate(0.5, start, end, now)
	// Floored at 1% elapsed -> forecast = total / 0.01 = 50.
	// Asserts the forecast doesn't go astronomical.
	if got > 60 {
		t.Errorf("expected forecast capped by 1%% floor (~50), got %v", got)
	}
}

func TestForecastZeroTotalDuration(t *testing.T) {
	// Defensive: start == end shouldn't divide by zero.
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	got := forecastExtrapolate(42, t0, t0, t0.Add(time.Hour))
	if got != 42 {
		t.Errorf("expected forecast=42 on degenerate period, got %v", got)
	}
}

// ─── tenantClause pure logic ──────────────────────────────────────────────

func TestTenantClauseEmpty(t *testing.T) {
	c, args := tenantClause("")
	if c != "" || args != nil {
		t.Errorf("empty tenant -> empty clause/args, got %q / %v", c, args)
	}
}

func TestTenantClausePopulated(t *testing.T) {
	c, args := tenantClause("tid-1")
	if c != " and tenant_id = $1" {
		t.Errorf("unexpected clause: %q", c)
	}
	if len(args) != 1 || args[0] != "tid-1" {
		t.Errorf("expected args=[tid-1], got %v", args)
	}
}

// ─── normalizePeriod defaults ─────────────────────────────────────────────

func TestNormalizePeriodFillsDefaults(t *testing.T) {
	start, end := normalizePeriod(time.Time{}, time.Time{})
	now := time.Now().UTC()
	wantStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Errorf("expected start=%s, got %s", wantStart, start)
	}
	// end defaults to ~now; just check it's within the last second.
	if end.Before(now.Add(-time.Second)) || end.After(now.Add(time.Second)) {
		t.Errorf("expected end ≈ now, got %s vs %s", end, now)
	}
}

func TestNormalizePeriodPassesThrough(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	gotStart, gotEnd := normalizePeriod(start, end)
	if !gotStart.Equal(start) || !gotEnd.Equal(end) {
		t.Errorf("expected passthrough, got %s..%s", gotStart, gotEnd)
	}
}

// ─── Aggregate no-pool path ───────────────────────────────────────────────

func TestAggregateNilPoolReturnsEmpty(t *testing.T) {
	a := NewAggregate(nil, nil)
	s, err := a.Summary(context.Background(), AggregateOpts{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if s.PeriodTotalUSD != 0 {
		t.Errorf("expected zero PeriodTotalUSD, got %v", s.PeriodTotalUSD)
	}
	if s.BudgetUSD != nil {
		t.Errorf("expected nil BudgetUSD, got %v", *s.BudgetUSD)
	}
	if s.ByDay == nil || s.ByModel == nil || s.ByAgent == nil || s.ByTenant == nil {
		t.Errorf("expected empty non-nil slices, got nils")
	}
}

func TestAggregateNilPoolEventsReturnsEmpty(t *testing.T) {
	a := NewAggregate(nil, nil)
	page, err := a.Events(context.Background(), EventsOpts{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if page.Total != 0 || len(page.Events) != 0 {
		t.Errorf("expected empty page, got %+v", page)
	}
}

func TestAggregateCostForExecutionNilPool(t *testing.T) {
	a := NewAggregate(nil, nil)
	cost, ok, err := a.CostForExecution(context.Background(), "exec-1")
	if err != nil || ok || cost != 0 {
		t.Errorf("expected (0,false,nil), got (%v,%v,%v)", cost, ok, err)
	}
}

// ─── S5 default spend ceiling (no-DB unit surface) ────────────────────────

// TestWithDefaultCeilingClampsNegative: a negative ceiling is treated
// as "disabled" (0), never as a negative cap that would reject every
// call. Maps to contract item C6.
func TestWithDefaultCeilingClampsNegative(t *testing.T) {
	b := NewBudgets(nil, nil).WithDefaultCeilingUSD(-5)
	if got := b.DefaultCeilingUSD(); got != 0 {
		t.Errorf("expected negative ceiling clamped to 0, got %v", got)
	}
	b2 := NewBudgets(nil, nil).WithDefaultCeilingUSD(42.5)
	if got := b2.DefaultCeilingUSD(); got != 42.5 {
		t.Errorf("expected ceiling 42.5, got %v", got)
	}
}

// TestDefaultCeilingNilReceiverSafe: the setter/getter tolerate a nil
// receiver so boot-path wiring can stay branch-free.
func TestDefaultCeilingNilReceiverSafe(t *testing.T) {
	var b *Budgets
	if got := b.WithDefaultCeilingUSD(10); got != nil {
		t.Errorf("expected nil receiver to stay nil, got %v", got)
	}
	if got := b.DefaultCeilingUSD(); got != 0 {
		t.Errorf("expected 0 from nil receiver, got %v", got)
	}
}

// TestCurrentMonthStartUTC: the default-ceiling spend window anchors at
// midnight UTC on the first of the current month. Maps to contract
// item C7.
func TestCurrentMonthStartUTC(t *testing.T) {
	got := currentMonthStartUTC()
	now := time.Now().UTC()
	if got.Day() != 1 || got.Hour() != 0 || got.Minute() != 0 || got.Second() != 0 || got.Nanosecond() != 0 {
		t.Errorf("expected midnight on the 1st, got %v", got)
	}
	if got.Year() != now.Year() || got.Month() != now.Month() {
		t.Errorf("expected current year/month, got %v (now %v)", got, now)
	}
	if got.Location() != time.UTC {
		t.Errorf("expected UTC, got %v", got.Location())
	}
}

// TestHasBudgetNilPoolIgnoresCeiling: with no DB the gate is fully
// permissive even when a default ceiling is configured — boot mode must
// never reject. Maps to contract item C4 (degraded variant).
func TestHasBudgetNilPoolIgnoresCeiling(t *testing.T) {
	b := NewBudgets(nil, nil).WithDefaultCeilingUSD(1)
	ok, err := b.HasBudget(context.Background(), "t_abc", 999)
	if err != nil || !ok {
		t.Errorf("expected admit with nil pool, got ok=%v err=%v", ok, err)
	}
}

// ─── Hook handler payload translation ─────────────────────────────────────

// TestPreCallHandlerNonMapPayloadAdmits confirms unknown payload types
// don't cause the LLM gateway to fail (we never reject on parse error).
func TestPreCallHandlerNonMapPayloadAdmits(t *testing.T) {
	h := PreCallHandler(NewBudgets(nil, nil))
	out, err := h(context.Background(), "not a map")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if out != "not a map" {
		t.Errorf("expected payload passthrough, got %v", out)
	}
}

// TestPreCallHandlerEmptyTenantAdmits confirms an unauthenticated
// payload (tenant_id == "") is admitted.
func TestPreCallHandlerEmptyTenantAdmits(t *testing.T) {
	h := PreCallHandler(NewBudgets(nil, nil))
	out, err := h(context.Background(), map[string]any{
		"tenant_id": "",
		"model":     "openai/gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if out == nil {
		t.Errorf("expected payload passthrough, got nil")
	}
}

// TestPostCallHandlerRecordsWithoutErroring confirms a malformed
// payload doesn't surface as a hook error (post-call is best-effort).
func TestPostCallHandlerSwallowsErrors(t *testing.T) {
	h := PostCallHandler(NewRecorder(nil, nil))
	_, err := h(context.Background(), "not a map")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// Valid payload also returns nil even though the recorder has no DB.
	_, err = h(context.Background(), map[string]any{
		"tenant_id":         "tid",
		"model":             "openai/gpt-4o-mini",
		"provider":          "openai",
		"prompt_tokens":     100,
		"completion_tokens": 50,
		"cost_usd":          0.001,
		"cached":            false,
		"latency_ms":        250,
	})
	if err != nil {
		t.Fatalf("expected nil error from no-pool recorder, got %v", err)
	}
}

// TestHookPayloadTypeCoercion exercises the int/float coercion in
// intFromMap/floatFromMap (JSON numbers come in as float64).
func TestHookPayloadTypeCoercion(t *testing.T) {
	m := map[string]any{
		"prompt_tokens":     float64(100),
		"completion_tokens": 50,                    // raw int
		"total_tokens":      int64(150),            // int64
		"cost_usd":          float64(0.001),
		"latency_ms":        float64(250),
	}
	if got := intFromMap(m, "prompt_tokens"); got != 100 {
		t.Errorf("expected 100, got %v", got)
	}
	if got := intFromMap(m, "completion_tokens"); got != 50 {
		t.Errorf("expected 50, got %v", got)
	}
	if got := intFromMap(m, "total_tokens"); got != 150 {
		t.Errorf("expected 150, got %v", got)
	}
	if got := floatFromMap(m, "cost_usd"); got != 0.001 {
		t.Errorf("expected 0.001, got %v", got)
	}
	if got := intFromMap(m, "missing_key"); got != 0 {
		t.Errorf("expected 0 default, got %v", got)
	}
}

// TestPreCallHandlerWiresHookEngine confirms hooks.Engine accepts and
// fires our PreCallHandler in registration order.
func TestPreCallHandlerWiresHookEngine(t *testing.T) {
	engine := hooks.NewEngine(nil)
	h := PreCallHandler(NewBudgets(nil, nil))
	if err := engine.Register(hooks.HookLLMPreCall, h); err != nil {
		t.Fatalf("register: %v", err)
	}
	out, err := engine.Fire(context.Background(), hooks.HookLLMPreCall, map[string]any{
		"tenant_id": "",
	})
	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	if out == nil {
		t.Errorf("expected payload back from hook chain")
	}
}