// SPDX-License-Identifier: Apache-2.0

// aggregate.go — read-side aggregations for the dashboard cost surface.
//
// Aggregate.Summary powers GET /api/v1/cost. It returns the dashboard's
// six panels (period total, previous-period total, forecast, by-day,
// by-model, by-agent, by-tenant) plus the active budget cap (if any).
//
// Aggregate.Events powers GET /api/v1/cost/events: a paginated list
// of raw rows for the operator's "event log" view.
//
// Both methods short-circuit to empty/zero output when the pool is
// nil so a runtime booted without DB still serves the dashboard
// skeleton.

package cost

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Summary is the response payload for GET /api/v1/cost. Field names
// match CostSummarySchema in apps/dashboard/src/lib/api.ts.
//
// BudgetUSD is *float64 (nullable JSON) because a tenant without a
// budget renders as JSON null in the dashboard, distinct from
// budget=0 which is a real (rejected) configuration.
type Summary struct {
	PeriodTotalUSD   float64
	PreviousTotalUSD float64
	BudgetUSD        *float64
	ForecastUSD      float64
	ByDay            []DayPoint
	ByModel          []ModelTotal
	ByAgent          []AgentTotal
	ByTenant         []TenantTotal
}

// DayPoint is one row of the by_day breakdown.
type DayPoint struct {
	Date    string // YYYY-MM-DD UTC
	CostUSD float64
}

// ModelTotal is one row of by_model.
type ModelTotal struct {
	Model   string
	CostUSD float64
}

// AgentTotal is one row of by_agent.
type AgentTotal struct {
	Agent   string
	CostUSD float64
}

// TenantTotal is one row of by_tenant. TenantName is nullable
// (tenant deleted or never named).
type TenantTotal struct {
	TenantID   string
	TenantName *string
	CostUSD    float64
}

// EventRow is one row of the events list. Matches CostEventSchema.
//
// TenantID, APIKeyID, Agent are *string for nullable JSON. OccurredAt
// is ISO8601 (RFC3339Nano UTC) so the dashboard can sort and bucket
// without re-parsing.
type EventRow struct {
	ID               string
	TenantID         *string
	APIKeyID         *string
	Model            string
	Provider         string
	Agent            *string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CostUSD          float64
	Cached           bool
	LatencyMS        int
	OccurredAt       time.Time
}

// EventList is the paginated response for GET /api/v1/cost/events.
type EventList struct {
	Events  []EventRow
	Total   int64
	HasMore bool
}

// Aggregate holds the read-side handles.
type Aggregate struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewAggregate constructs an Aggregate. Pool/log may be nil.
func NewAggregate(pool *pgxpool.Pool, log *slog.Logger) *Aggregate {
	if log == nil {
		log = slog.Default()
	}
	return &Aggregate{pool: pool, log: log}
}

// Summary computes the dashboard's cost summary for [PeriodStart,
// PeriodEnd). When PeriodStart/PeriodEnd are zero, defaults to
// [start-of-current-month, now).
//
// TenantID, when non-empty, scopes every panel to a single tenant
// (including the by_tenant breakdown which then has exactly one row).
//
// Forecast uses linear extrapolation: period_total / elapsed_fraction.
// Elapsed fraction is (now - PeriodStart) / (PeriodEnd - PeriodStart).
// If PeriodEnd is in the past (closed period) we return PeriodTotal
// as the forecast (no extrapolation possible).
func (a *Aggregate) Summary(ctx context.Context, opts AggregateOpts) (Summary, error) {
	out := Summary{
		ByDay:    []DayPoint{},
		ByModel:  []ModelTotal{},
		ByAgent:  []AgentTotal{},
		ByTenant: []TenantTotal{},
	}
	if a == nil || a.pool == nil {
		return out, nil
	}

	periodStart, periodEnd := normalizePeriod(opts.PeriodStart, opts.PeriodEnd)
	previousStart := periodStart.AddDate(0, -1, 0)
	previousEnd := periodStart

	// Build tenant scope clauses (used by every panel below).
	tenantWhere, tenantArgs := tenantClause(opts.TenantID)

	// 1. Period total.
	periodTotal, err := a.sumCost(ctx, periodStart, periodEnd, tenantWhere, tenantArgs)
	if err != nil {
		return out, err
	}
	out.PeriodTotalUSD = periodTotal

	// 2. Previous period total. For trend deltas in the dashboard.
	prevTotal, err := a.sumCost(ctx, previousStart, previousEnd, tenantWhere, tenantArgs)
	if err != nil {
		return out, err
	}
	out.PreviousTotalUSD = prevTotal

	// 3. Forecast. Linear extrapolation from elapsed fraction.
	out.ForecastUSD = forecastExtrapolate(periodTotal, periodStart, periodEnd, time.Now().UTC())

	// 4. Budget (nullable). Only meaningful when scoped to one tenant
	// — the cross-tenant view doesn't have a single budget number.
	if opts.TenantID != "" {
		var monthlyUSD float64
		err := a.pool.QueryRow(ctx, `
            select monthly_usd from suite_budgets where tenant_id = $1
        `, opts.TenantID).Scan(&monthlyUSD)
		if err == nil {
			b := monthlyUSD
			out.BudgetUSD = &b
		} else if err != pgx.ErrNoRows {
			return out, fmt.Errorf("cost: load budget for summary: %w", err)
		}
	}

	// 5. by_day. UTC date buckets across the period. We emit only
	// dates that have rows — the dashboard fills gaps with zeros.
	out.ByDay, err = a.byDay(ctx, periodStart, periodEnd, tenantWhere, tenantArgs)
	if err != nil {
		return out, err
	}

	// 6. by_model.
	out.ByModel, err = a.byModel(ctx, periodStart, periodEnd, tenantWhere, tenantArgs)
	if err != nil {
		return out, err
	}

	// 7. by_agent. Excludes events without an agent (NULL); the
	// dashboard renders those under "(no agent)" if needed.
	out.ByAgent, err = a.byAgent(ctx, periodStart, periodEnd, tenantWhere, tenantArgs)
	if err != nil {
		return out, err
	}

	// 8. by_tenant. Joins suite_tenants for the display name.
	out.ByTenant, err = a.byTenant(ctx, periodStart, periodEnd, tenantWhere, tenantArgs)
	if err != nil {
		return out, err
	}

	return out, nil
}

// Events returns a paginated list of cost events filtered by tenant /
// model / time range.
func (a *Aggregate) Events(ctx context.Context, opts EventsOpts) (EventList, error) {
	out := EventList{Events: []EventRow{}}
	if a == nil || a.pool == nil {
		return out, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	conds := []string{}
	args := []any{}
	bind := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if opts.TenantID != "" {
		conds = append(conds, "tenant_id = "+bind(opts.TenantID))
	}
	if opts.Model != "" {
		conds = append(conds, "model = "+bind(opts.Model))
	}
	if !opts.From.IsZero() {
		conds = append(conds, "occurred_at >= "+bind(opts.From))
	}
	if !opts.To.IsZero() {
		conds = append(conds, "occurred_at < "+bind(opts.To))
	}
	where := ""
	if len(conds) > 0 {
		where = "where " + strings.Join(conds, " and ")
	}

	// Count first so the dashboard can render pagination controls.
	if err := a.pool.QueryRow(ctx,
		"select count(*) from suite_cost_events "+where,
		args...,
	).Scan(&out.Total); err != nil {
		return out, fmt.Errorf("cost: count events: %w", err)
	}

	pageArgs := append([]any{}, args...)
	pageArgs = append(pageArgs, limit, offset)
	limitIdx := len(pageArgs) - 1
	offsetIdx := len(pageArgs)

	rows, err := a.pool.Query(ctx, `
        select
            id::text,
            tenant_id::text,
            api_key_id::text,
            model, provider, agent,
            prompt_tokens, completion_tokens, total_tokens,
            cost_usd, cached, latency_ms, occurred_at
          from suite_cost_events
        `+where+`
         order by occurred_at desc
         limit $`+strconv.Itoa(limitIdx)+`
         offset $`+strconv.Itoa(offsetIdx),
		pageArgs...,
	)
	if err != nil {
		return out, fmt.Errorf("cost: list events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			ev               EventRow
			tenantID         *string
			apiKeyID         *string
			agent            *string
		)
		if err := rows.Scan(
			&ev.ID,
			&tenantID,
			&apiKeyID,
			&ev.Model,
			&ev.Provider,
			&agent,
			&ev.PromptTokens,
			&ev.CompletionTokens,
			&ev.TotalTokens,
			&ev.CostUSD,
			&ev.Cached,
			&ev.LatencyMS,
			&ev.OccurredAt,
		); err != nil {
			return out, fmt.Errorf("cost: scan event: %w", err)
		}
		ev.TenantID = tenantID
		ev.APIKeyID = apiKeyID
		ev.Agent = agent
		out.Events = append(out.Events, ev)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("cost: iterate events: %w", err)
	}

	out.HasMore = int64(offset+len(out.Events)) < out.Total
	return out, nil
}

// CostForExecution returns the summed cost_usd attributed to a single
// AF execution by joining suite_cost_events to suite_gateway_requests
// via af_execution_id. Returns (0, false) on no match.
//
// Best-effort matching: events log cost; gateway requests log exec id.
// We approximate by matching events to the request via tenant + time
// proximity to the gateway request. Phase 7.1 may add an explicit
// af_execution_id column on suite_cost_events for exact matching.
func (a *Aggregate) CostForExecution(ctx context.Context, executionID string) (float64, bool, error) {
	if a == nil || a.pool == nil || executionID == "" {
		return 0, false, nil
	}
	// Approximation: sum events within 60s of the gateway request that
	// recorded this execution, scoped to the same tenant. If multiple
	// gateway rows share the same af_execution_id (retries) we sum
	// across all of them — operator wants "what did this run cost".
	var cost float64
	var matched bool
	err := a.pool.QueryRow(ctx, `
        with req as (
            select tenant_id, min(created_at) as t0, max(created_at) as t1
              from suite_gateway_requests
             where af_execution_id = $1
             group by tenant_id
        )
        select coalesce(sum(e.cost_usd), 0), count(*) > 0
          from req
          join suite_cost_events e
            on e.tenant_id is not distinct from req.tenant_id
           and e.occurred_at >= req.t0 - interval '5 seconds'
           and e.occurred_at <= req.t1 + interval '60 seconds'
    `, executionID).Scan(&cost, &matched)
	if err != nil {
		return 0, false, fmt.Errorf("cost: load by execution: %w", err)
	}
	return cost, matched, nil
}

// ─── internals ────────────────────────────────────────────────────────────

// normalizePeriod fills in default bounds: start-of-current-month → now.
func normalizePeriod(start, end time.Time) (time.Time, time.Time) {
	now := time.Now().UTC()
	if start.IsZero() {
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	} else {
		start = start.UTC()
	}
	if end.IsZero() {
		end = now
	} else {
		end = end.UTC()
	}
	return start, end
}

// tenantClause builds an "and tenant_id = $N" fragment for the
// optional tenant scope. Returns (clauseFragment, []arg) where the
// fragment is empty when no tenant is scoped.
//
// Returned clause is "" or " and tenant_id = $1" — callers prefix
// their own "where" and append additional bound args at indices >= 2.
func tenantClause(tenantID string) (string, []any) {
	if tenantID == "" {
		return "", nil
	}
	return " and tenant_id = $1", []any{tenantID}
}

// sumCost runs `select coalesce(sum(cost_usd), 0) ... where occurred_at
// in [start, end) [and tenant_id = $1]`.
//
// The tenantWhere/tenantArgs come from tenantClause(). When non-empty,
// $1 binds the tenant; period bounds use $2/$3. When empty, period
// uses $1/$2.
func (a *Aggregate) sumCost(ctx context.Context, start, end time.Time, tenantWhere string, tenantArgs []any) (float64, error) {
	args := append([]any{}, tenantArgs...)
	startIdx := strconv.Itoa(len(args) + 1)
	args = append(args, start)
	endIdx := strconv.Itoa(len(args) + 1)
	args = append(args, end)

	q := `
        select coalesce(sum(cost_usd), 0)
          from suite_cost_events
         where occurred_at >= $` + startIdx + `
           and occurred_at < $` + endIdx + tenantWhere

	var v float64
	if err := a.pool.QueryRow(ctx, q, args...).Scan(&v); err != nil {
		return 0, fmt.Errorf("cost: sum: %w", err)
	}
	return v, nil
}

// forecastExtrapolate computes a linear forecast for the period.
//
// If now is past periodEnd, the period is closed — forecast equals
// the observed total. Otherwise forecast = total / elapsedFraction,
// where elapsedFraction is clipped to [epsilon, 1].
//
// A near-zero elapsed fraction (period just started) would produce an
// astronomical forecast; we floor at 1% so the dashboard renders a
// stable upper bound rather than 100x the current spend.
func forecastExtrapolate(periodTotal float64, periodStart, periodEnd, now time.Time) float64 {
	if !now.Before(periodEnd) {
		return periodTotal
	}
	total := periodEnd.Sub(periodStart).Seconds()
	if total <= 0 {
		return periodTotal
	}
	elapsed := now.Sub(periodStart).Seconds()
	if elapsed <= 0 {
		return periodTotal
	}
	fraction := elapsed / total
	const minFraction = 0.01
	if fraction < minFraction {
		fraction = minFraction
	}
	if fraction > 1 {
		fraction = 1
	}
	return periodTotal / fraction
}

func (a *Aggregate) byDay(ctx context.Context, start, end time.Time, tenantWhere string, tenantArgs []any) ([]DayPoint, error) {
	args := append([]any{}, tenantArgs...)
	startIdx := strconv.Itoa(len(args) + 1)
	args = append(args, start)
	endIdx := strconv.Itoa(len(args) + 1)
	args = append(args, end)

	q := `
        select date_trunc('day', occurred_at)::date as d,
               coalesce(sum(cost_usd), 0)
          from suite_cost_events
         where occurred_at >= $` + startIdx + `
           and occurred_at < $` + endIdx + tenantWhere + `
         group by d
         order by d
    `
	rows, err := a.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("cost: by_day: %w", err)
	}
	defer rows.Close()
	out := make([]DayPoint, 0)
	for rows.Next() {
		var d time.Time
		var cost float64
		if err := rows.Scan(&d, &cost); err != nil {
			return nil, fmt.Errorf("cost: by_day scan: %w", err)
		}
		out = append(out, DayPoint{
			Date:    d.UTC().Format("2006-01-02"),
			CostUSD: cost,
		})
	}
	return out, rows.Err()
}

func (a *Aggregate) byModel(ctx context.Context, start, end time.Time, tenantWhere string, tenantArgs []any) ([]ModelTotal, error) {
	args := append([]any{}, tenantArgs...)
	startIdx := strconv.Itoa(len(args) + 1)
	args = append(args, start)
	endIdx := strconv.Itoa(len(args) + 1)
	args = append(args, end)

	q := `
        select model, coalesce(sum(cost_usd), 0) as c
          from suite_cost_events
         where occurred_at >= $` + startIdx + `
           and occurred_at < $` + endIdx + tenantWhere + `
         group by model
         order by c desc
    `
	rows, err := a.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("cost: by_model: %w", err)
	}
	defer rows.Close()
	out := make([]ModelTotal, 0)
	for rows.Next() {
		var m string
		var cost float64
		if err := rows.Scan(&m, &cost); err != nil {
			return nil, fmt.Errorf("cost: by_model scan: %w", err)
		}
		out = append(out, ModelTotal{Model: m, CostUSD: cost})
	}
	return out, rows.Err()
}

func (a *Aggregate) byAgent(ctx context.Context, start, end time.Time, tenantWhere string, tenantArgs []any) ([]AgentTotal, error) {
	args := append([]any{}, tenantArgs...)
	startIdx := strconv.Itoa(len(args) + 1)
	args = append(args, start)
	endIdx := strconv.Itoa(len(args) + 1)
	args = append(args, end)

	q := `
        select agent, coalesce(sum(cost_usd), 0) as c
          from suite_cost_events
         where occurred_at >= $` + startIdx + `
           and occurred_at < $` + endIdx + `
           and agent is not null` + tenantWhere + `
         group by agent
         order by c desc
    `
	rows, err := a.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("cost: by_agent: %w", err)
	}
	defer rows.Close()
	out := make([]AgentTotal, 0)
	for rows.Next() {
		var ag string
		var cost float64
		if err := rows.Scan(&ag, &cost); err != nil {
			return nil, fmt.Errorf("cost: by_agent scan: %w", err)
		}
		out = append(out, AgentTotal{Agent: ag, CostUSD: cost})
	}
	return out, rows.Err()
}

func (a *Aggregate) byTenant(ctx context.Context, start, end time.Time, tenantWhere string, tenantArgs []any) ([]TenantTotal, error) {
	args := append([]any{}, tenantArgs...)
	startIdx := strconv.Itoa(len(args) + 1)
	args = append(args, start)
	endIdx := strconv.Itoa(len(args) + 1)
	args = append(args, end)

	// Left join suite_tenants for display name. We exclude rows with
	// NULL tenant_id (those are unauth events; operator panel rolls
	// them up separately if needed).
	q := `
        select e.tenant_id::text,
               t.name,
               coalesce(sum(e.cost_usd), 0) as c
          from suite_cost_events e
          left join suite_tenants t on t.id = e.tenant_id
         where e.occurred_at >= $` + startIdx + `
           and e.occurred_at < $` + endIdx + `
           and e.tenant_id is not null` + tenantWhere + `
         group by e.tenant_id, t.name
         order by c desc
    `
	// tenantWhere from tenantClause uses bare "tenant_id" without
	// alias; we need "e.tenant_id" here. Inline-substitute.
	if tenantWhere != "" {
		q = strings.Replace(q, tenantWhere, strings.ReplaceAll(tenantWhere, "tenant_id", "e.tenant_id"), 1)
	}

	rows, err := a.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("cost: by_tenant: %w", err)
	}
	defer rows.Close()
	out := make([]TenantTotal, 0)
	for rows.Next() {
		var tid string
		var name *string
		var cost float64
		if err := rows.Scan(&tid, &name, &cost); err != nil {
			return nil, fmt.Errorf("cost: by_tenant scan: %w", err)
		}
		out = append(out, TenantTotal{TenantID: tid, TenantName: name, CostUSD: cost})
	}
	return out, rows.Err()
}