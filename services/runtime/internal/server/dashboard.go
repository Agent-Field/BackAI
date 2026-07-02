// SPDX-License-Identifier: Apache-2.0

// Package server — dashboard.go wires the read-only REST endpoints that
// power the AF Stack dashboard (apps/dashboard).
//
// Every shape returned by these handlers MUST match the zod schemas in
// apps/dashboard/src/lib/api.ts exactly (snake_case field names). The
// dashboard validates responses with safeParse and surfaces SCHEMA_MISMATCH
// errors loudly, so drift here breaks the UI.
//
// All handlers tolerate the empty-DB case (zero rows / nil pool) — they
// return zeros and empty arrays so the dashboard renders skeletons rather
// than error envelopes during fresh installs.
//
// OTel: each handler opens a server-kind span named "dashboard.<route>"
// so traces show the dashboard-attributable load distinctly.
package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Agent-Field/backai/services/runtime/internal/cost"
)

// tracerName is the OTel tracer name used for dashboard spans. Kept
// distinct from the HTTP-middleware tracer so dashboards can split them.
const dashboardTracerName = "af-stack/dashboard"

func (s *Server) dashTracer() trace.Tracer {
	return otel.Tracer(dashboardTracerName)
}

// ─── Common types ─────────────────────────────────────────────────────────

// runRecord is the JSON shape returned for a single run. Field tags match
// RunSchema in apps/dashboard/src/lib/api.ts exactly. Optional fields use
// `omitempty` so absent values stay absent (zod treats undefined as fine
// for `.optional()`).
type runRecord struct {
	ID         string  `json:"id"`
	Agent      string  `json:"agent"`
	Status     string  `json:"status"`
	TenantID   string  `json:"tenant_id,omitempty"`
	TenantName string  `json:"tenant_name,omitempty"`
	StartedAt  string  `json:"started_at"`
	DurationMS *int64  `json:"duration_ms,omitempty"`
	CostUSD    float64 `json:"cost_usd"`
	// Input/output/error are intentionally always omitted in Phase 4 — we
	// don't yet persist them. Phase 7 wires this up.
	Input  any    `json:"input,omitempty"`
	Output any    `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// runListResponse matches RunListSchema.
type runListResponse struct {
	Runs    []runRecord `json:"runs"`
	Total   int64       `json:"total"`
	HasMore bool        `json:"has_more"`
}

// ─── GET /api/v1/runs ─────────────────────────────────────────────────────

// handleListRuns returns recent agent invocations recorded in
// suite_gateway_requests. The gateway already logs every call there, so
// this is the canonical run history during Phase 4 (real run objects with
// step traces land in Phase 7).
//
// Query params:
//
//	agent   — exact match on endpoint (e.g. "sample.echo")
//	tenant  — exact match on tenant_id (string-compared; UUIDs accepted)
//	status  — "succeeded" or "failed" (2xx vs not-2xx); other values ignored
//	limit   — 1..200, default 50
//	offset  — >=0, default 0
//
// Rows are ordered by created_at DESC. has_more is true when the next
// page would have at least one row (total > offset+limit).
func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.runs.list")
	defer span.End()

	q := r.URL.Query()
	limit, offset := parsePaging(q.Get("limit"), q.Get("offset"))
	agent := strings.TrimSpace(q.Get("agent"))
	tenant := strings.TrimSpace(q.Get("tenant"))
	status := strings.TrimSpace(q.Get("status"))

	span.SetAttributes(
		attribute.Int("paging.limit", limit),
		attribute.Int("paging.offset", offset),
		attribute.String("filter.agent", agent),
		attribute.String("filter.tenant", tenant),
		attribute.String("filter.status", status),
	)

	resp, err := s.queryRuns(ctx, runQuery{
		Agent:  agent,
		Tenant: tenant,
		Status: status,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		s.log.Error("list runs failed", "error", err)
		span.RecordError(err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type runQuery struct {
	Agent  string
	Tenant string
	Status string
	Limit  int
	Offset int
}

// queryRuns reads rows from suite_gateway_requests. Returns an empty
// response when the DB is not configured (no DB = no runs yet).
func (s *Server) queryRuns(ctx context.Context, q runQuery) (runListResponse, error) {
	out := runListResponse{Runs: []runRecord{}}
	if s.db == nil || s.db.Pool == nil {
		return out, nil
	}

	// Build the WHERE clause. We filter to gateway-request rows that
	// represent billable work — agent execute forwards and LLM gateway
	// calls — so this view is "runs" rather than every HTTP hit the
	// gateway recorded (health checks, admin traffic, ...).
	//
	// Column references use the `r` alias so the same clause can be
	// reused by both the count-only query (FROM suite_gateway_requests r)
	// and the rows query that joins to suite_cost_events.
	conds := []string{runEndpointCond}
	args := []any{}
	bind := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if q.Agent != "" {
		// Match both sync and async endpoint variants for the requested
		// agent call (e.g. "/api/v1/execute/sample.echo" and
		// "/api/v1/execute/async/sample.echo"), plus LLM rows whose
		// caller label (from X-AF-Reasoner) matches.
		syncBind := bind("/api/v1/execute/" + q.Agent)
		asyncBind := bind("/api/v1/execute/async/" + q.Agent)
		labelBind := bind(q.Agent)
		conds = append(conds, "(r.endpoint = "+syncBind+" or r.endpoint = "+asyncBind+" or r.agent_label = "+labelBind+")")
	}
	if q.Tenant != "" {
		conds = append(conds, "r.tenant_id::text = "+bind(q.Tenant))
	}
	switch q.Status {
	case "succeeded":
		conds = append(conds, "r.status_code >= 200 and r.status_code < 300")
	case "failed":
		conds = append(conds, "(r.status_code is null or r.status_code < 200 or r.status_code >= 300)")
	}
	where := "where " + strings.Join(conds, " and ")

	// suite_gateway_requests is RLS-guarded (tenant_isolation policy).
	// This is an operator-facing, cross-tenant view served without a
	// tenant binding, so read through a short transaction with
	// app.bypass_rls=on — the same pattern as aggregation_endpoints.go
	// and admin_anchors.go. Without it the policy filters every row and
	// the Runs page renders empty.
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only tx; rollback is the cleanup path
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		return out, err
	}

	// Total count for pagination.
	var total int64
	countSQL := "select count(*) from suite_gateway_requests r " + where
	if err := tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return out, err
	}
	out.Total = total

	// Page of rows.
	//
	// cost_usd is approximated by summing suite_cost_events rows that
	// share the same tenant and arrived within a small window of the
	// gateway request. The gateway doesn't yet stamp af_execution_id
	// on cost events (Phase 7.1 will), so this is best-effort. Empty
	// matches collapse to 0.
	pageArgs := append([]any{}, args...)
	pageArgs = append(pageArgs, q.Limit, q.Offset)
	limitIdx := len(pageArgs) - 1
	offsetIdx := len(pageArgs)
	rowsSQL := `
        select
            r.id,
            coalesce(r.tenant_id::text, '') as tenant_id_text,
            coalesce(t.name, '')            as tenant_name,
            r.endpoint,
            coalesce(r.agent_label, '')     as agent_label,
            r.status_code,
            r.duration_ms,
            r.created_at,
            coalesce((
              select sum(e.cost_usd)
                from suite_cost_events e
               where e.tenant_id is not distinct from r.tenant_id
                 and e.occurred_at >= r.created_at - interval '5 seconds'
                 and e.occurred_at <= r.created_at + interval '60 seconds'
            ), 0) as cost_usd
          from suite_gateway_requests r
          left join suite_tenants t on t.id = r.tenant_id ` + where +
		" order by r.created_at desc limit $" + strconv.Itoa(limitIdx) +
		" offset $" + strconv.Itoa(offsetIdx)

	rows, err := tx.Query(ctx, rowsSQL, pageArgs...)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id         pgtype.UUID
			tenantID   string
			tenantName string
			endpoint   string
			agentLabel string
			statusCode pgtype.Int4
			durationMS pgtype.Int4
			createdAt  time.Time
			costUSD    float64
		)
		if err := rows.Scan(&id, &tenantID, &tenantName, &endpoint, &agentLabel, &statusCode, &durationMS, &createdAt, &costUSD); err != nil {
			return out, err
		}
		agentName := agentLabel
		if agentName == "" {
			agentName = agentFromEndpoint(endpoint)
		}
		rec := runRecord{
			ID:        uuidString(id),
			Agent:     agentName,
			Status:    classifyStatus(statusCode),
			StartedAt: createdAt.UTC().Format(time.RFC3339Nano),
			CostUSD:   costUSD,
		}
		if tenantID != "" {
			rec.TenantID = tenantID
		}
		if tenantName != "" {
			rec.TenantName = tenantName
		}
		if durationMS.Valid {
			v := int64(durationMS.Int32)
			rec.DurationMS = &v
		}
		out.Runs = append(out.Runs, rec)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	out.HasMore = int64(q.Offset+len(out.Runs)) < total
	return out, nil
}

// runEndpointCond selects the gateway-request rows that count as "runs":
// agent execute forwards plus LLM gateway calls. Shared by queryRuns and
// the home-overview failure counter so the two views agree on what a run is.
const runEndpointCond = "(r.endpoint like '/api/v1/execute/%' or r.endpoint like '/api/v1/llm/%')"

// agentFromEndpoint strips the /api/v1/execute/ (or async) prefix to
// recover the agent call name (e.g. "sample.echo"). LLM gateway rows
// without an agent_label fall back to a stable "llm.<operation>" name.
// Returns the raw endpoint if no prefix matches.
func agentFromEndpoint(endpoint string) string {
	const sync = "/api/v1/execute/"
	const async = "/api/v1/execute/async/"
	const llm = "/api/v1/llm/"
	if strings.HasPrefix(endpoint, async) {
		return strings.TrimPrefix(endpoint, async)
	}
	if strings.HasPrefix(endpoint, sync) {
		return strings.TrimPrefix(endpoint, sync)
	}
	if strings.HasPrefix(endpoint, llm) {
		// "/api/v1/llm/chat/completions" → "llm.chat"
		rest := strings.TrimPrefix(endpoint, llm)
		if op, _, ok := strings.Cut(rest, "/"); ok && op != "" {
			return "llm." + op
		}
		if rest != "" {
			return "llm." + rest
		}
		return "llm"
	}
	return endpoint
}

// classifyStatus maps an HTTP status code to the dashboard's Run status
// enum. We only ever record terminal outcomes in suite_gateway_requests,
// so we use "succeeded" / "failed" (the queued/running/cancelled states
// only apply to async work tracked in Phase 5).
func classifyStatus(code pgtype.Int4) string {
	if !code.Valid {
		return "failed"
	}
	if code.Int32 >= 200 && code.Int32 < 300 {
		return "succeeded"
	}
	return "failed"
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	// pgtype.UUID's Bytes is a [16]byte — format as canonical 8-4-4-4-12.
	b := u.Bytes
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	j := 0
	for i := 0; i < 16; i++ {
		out[j] = hex[b[i]>>4]
		out[j+1] = hex[b[i]&0x0f]
		j += 2
		if i == 3 || i == 5 || i == 7 || i == 9 {
			out[j] = '-'
			j++
		}
	}
	return string(out)
}

// parsePaging clamps user input to safe bounds: limit in [1,200] (default
// 50), offset >= 0 (default 0).
func parsePaging(limitStr, offsetStr string) (int, int) {
	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 200 {
		limit = 200
	}
	if limit < 1 {
		limit = 1
	}
	offset := 0
	if offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
			offset = v
		}
	}
	return limit, offset
}

// ─── GET /api/v1/home/overview ────────────────────────────────────────────

// homeOverviewResponse matches HomeOverviewSchema.
type homeOverviewResponse struct {
	// KPI scalars rendered on the Home strip. See development/ux/pages/home.md §7
	// for the per-tile audit. The four scalars below close Gaps 1–4 from
	// development/ux/required-backend-gaps.md.
	RequestsPerMinute float64          `json:"requests_per_minute"`
	ErrorRate         float64          `json:"error_rate"`
	CostTodayUSD      float64          `json:"cost_today_usd"`
	QueueDepth        int              `json:"queue_depth"`
	LiveRuns          int              `json:"live_runs"`
	FailedRunsLast24h int              `json:"failed_runs_last_24h"`
	BudgetsAggregate  budgetsAggregate `json:"budgets_aggregate"`
	// Delta-vs-prior-window percentages, computed server-side from the
	// sparkline buckets (avg of last 4 buckets vs avg of buckets [-8:-4]).
	// Positive = increasing. Direction-semantics (good vs bad) are tile-specific
	// and handled by the dashboard renderer. NaN-safe: emitted as 0 when the
	// prior window has no signal.
	RequestDeltaPct float64 `json:"request_delta_pct"`
	ErrorDeltaPct   float64 `json:"error_delta_pct"`
	CostDeltaPct    float64 `json:"cost_delta_pct"`
	// 24-hour hourly buckets backing each KPI's sparkline.
	RequestSparkline        []float64         `json:"request_sparkline"`
	ErrorSparkline          []float64         `json:"error_sparkline"`
	CostSparkline           []float64         `json:"cost_sparkline"`
	QueueSparkline          []float64         `json:"queue_sparkline"`
	RecentRuns              []runRecord       `json:"recent_runs"`
	RecentWebhookDeliveries []webhookDelivery `json:"recent_webhook_deliveries"`
	Alerts                  []dashboardAlert  `json:"alerts"`
}

// budgetsAggregate summarises tenant budgets for the Home "Budget consumed %"
// tile (Gap 4). TenantsAtRisk counts budgets whose spend has already crossed
// the alert threshold; AvgConsumedPct is the mean spent/monthly ratio across
// all budgets (0..100). TenantCount is the denominator so the dashboard can
// render "— · 0 of 0 tenants" empty states without divide-by-zero.
type budgetsAggregate struct {
	TenantsAtRisk  int     `json:"tenants_at_risk"`
	AvgConsumedPct float64 `json:"avg_consumed_pct"`
	TenantCount    int     `json:"tenant_count"`
}

type webhookDelivery struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	Direction  string `json:"direction"`
	Status     string `json:"status"`
	OccurredAt string `json:"occurred_at"`
}

type dashboardAlert struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// handleHomeOverview returns the at-a-glance dashboard payload: live KPIs,
// 24h hourly sparklines, recent runs, and infrastructure alerts. All values
// degrade gracefully when the DB or AF dependency is unavailable.
func (s *Server) handleHomeOverview(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.home.overview")
	defer span.End()

	resp := homeOverviewResponse{
		// Always emit empty slices (not nil) so zod arrays validate even
		// when the DB is empty.
		RequestSparkline:        zeroSparkline(),
		ErrorSparkline:          zeroSparkline(),
		CostSparkline:           zeroSparkline(),
		QueueSparkline:          zeroSparkline(),
		RecentRuns:              []runRecord{},
		RecentWebhookDeliveries: []webhookDelivery{}, // Phase 10
		Alerts:                  []dashboardAlert{},
	}

	if s.db != nil && s.db.Pool != nil {
		now := time.Now().UTC()

		// requests_per_minute: count over last 60s / 1 minute window.
		var lastMin int64
		err := s.db.Pool.QueryRow(ctx,
			"select count(*) from suite_gateway_requests where created_at >= $1",
			now.Add(-1*time.Minute)).Scan(&lastMin)
		if err != nil {
			s.log.Warn("rpm query failed", "error", err)
			span.RecordError(err)
		}
		resp.RequestsPerMinute = float64(lastMin)

		// error_rate over last 60 minutes.
		var total60, failed60 int64
		err = s.db.Pool.QueryRow(ctx,
			"select "+
				"count(*), "+
				"count(*) filter (where status_code is null or status_code < 200 or status_code >= 300) "+
				"from suite_gateway_requests where created_at >= $1",
			now.Add(-60*time.Minute)).Scan(&total60, &failed60)
		if err != nil {
			s.log.Warn("error rate query failed", "error", err)
			span.RecordError(err)
		}
		if total60 > 0 {
			resp.ErrorRate = float64(failed60) / float64(total60)
		}

		// cost_today_usd sums suite_cost_events for the calendar day
		// (UTC) so far. Phase 7 ledger; pre-Phase-7 runs see 0.
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		var costToday float64
		err = s.db.Pool.QueryRow(ctx,
			"select coalesce(sum(cost_usd), 0) from suite_cost_events where occurred_at >= $1",
			todayStart).Scan(&costToday)
		if err != nil {
			s.log.Warn("cost today query failed", "error", err)
			span.RecordError(err)
		}
		resp.CostTodayUSD = costToday

		// Sparklines: bucket the last 24 hours into one-hour windows.
		if buckets, err := s.fetchHourlyBuckets(ctx, now); err == nil {
			resp.RequestSparkline = buckets.requests
			resp.ErrorSparkline = buckets.errors
			resp.CostSparkline = buckets.cost
			// Deltas: most-recent 4-hour window vs preceding 4-hour window.
			// Server-side so the dashboard never has to interpret the
			// sparkline shape itself.
			resp.RequestDeltaPct = sparklineDeltaPct(buckets.requests)
			resp.ErrorDeltaPct = sparklineDeltaPct(buckets.errors)
			resp.CostDeltaPct = sparklineDeltaPct(buckets.cost)
		} else {
			s.log.Warn("sparkline query failed", "error", err)
			span.RecordError(err)
		}

		// Recent runs: top 20.
		runs, err := s.queryRuns(ctx, runQuery{Limit: 20, Offset: 0})
		if err != nil {
			s.log.Warn("recent runs query failed", "error", err)
			span.RecordError(err)
		} else {
			resp.RecentRuns = runs.Runs
		}

		// Failed runs in the last 24 hours (Gap 3). Counts gateway requests
		// whose status_code is null or non-2xx, scoped to run endpoints
		// (execute + LLM gateway) so we don't conflate admin traffic with
		// run failures.
		var failed24 int64
		err = s.db.Pool.QueryRow(ctx,
			"select count(*) from suite_gateway_requests r "+
				"where "+runEndpointCond+" "+
				"and (status_code is null or status_code < 200 or status_code >= 300) "+
				"and created_at >= $1",
			now.Add(-24*time.Hour)).Scan(&failed24)
		if err != nil {
			s.log.Warn("failed runs 24h query failed", "error", err)
			span.RecordError(err)
		} else {
			resp.FailedRunsLast24h = int(failed24)
		}

		// Recent webhook deliveries: top 10. Reads suite_webhook_deliveries
		// across all tenants; maps DB enums to the dashboard's smaller
		// HomeOverviewSchema vocabulary (delivered/failed/pending, in/out).
		if hooks, err := s.fetchRecentWebhookDeliveries(ctx, 10); err == nil {
			resp.RecentWebhookDeliveries = hooks
		} else {
			s.log.Warn("recent webhook deliveries query failed", "error", err)
			span.RecordError(err)
		}
	}

	// Queue depth + live runs (Gaps 1 & 2). Both come from River via the
	// jobs Manager. The Home tile labelled "Live runs" counts River jobs in
	// the running state — this is the platform-wide async work signal, kept
	// distinct from RequestsPerMinute (sync gateway traffic).
	if s.jobs != nil {
		summary, err := s.jobs.Summary(ctx, 0)
		if err != nil {
			s.log.Warn("jobs summary failed", "error", err)
			span.RecordError(err)
		} else {
			resp.QueueDepth = summary.Pending
			resp.LiveRuns = summary.Running
		}
	}

	// Budgets aggregate (Gap 4). Walks every per-tenant budget once and
	// folds them into (TenantsAtRisk, AvgConsumedPct, TenantCount). Tolerates
	// the no-budget case — TenantCount=0 lets the dashboard render an empty
	// tile rather than a misleading 0%.
	if s.budgets != nil {
		if agg, err := computeBudgetsAggregate(ctx, s.budgets); err == nil {
			resp.BudgetsAggregate = agg
		} else {
			s.log.Warn("budgets aggregate failed", "error", err)
			span.RecordError(err)
		}
	}

	// Alerts: dependency health. We probe within a short timeout so the
	// dashboard request itself never hangs on a slow AF instance.
	probeCtx, probeCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer probeCancel()
	if s.af != nil {
		if _, err := s.af.Health(probeCtx); err != nil {
			resp.Alerts = append(resp.Alerts, dashboardAlert{
				ID:          "agentfield-unreachable",
				Severity:    "critical",
				Title:       "AgentField unreachable",
				Description: err.Error(),
			})
		}
	}
	if s.db != nil {
		if err := s.db.Health(probeCtx); err != nil {
			resp.Alerts = append(resp.Alerts, dashboardAlert{
				ID:          "database-unhealthy",
				Severity:    "critical",
				Title:       "Database unhealthy",
				Description: err.Error(),
			})
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type hourlyBuckets struct {
	requests []float64
	errors   []float64
	cost     []float64
}

// fetchHourlyBuckets returns 24-element arrays where index 0 is 23 hours
// ago and index 23 is the current hour. PG aggregates request/error
// counts from suite_gateway_requests and cost from suite_cost_events
// in two round-trips (one per table) — joining at the bucket level is
// cheaper than a hash join over 24h of rows.
func (s *Server) fetchHourlyBuckets(ctx context.Context, now time.Time) (hourlyBuckets, error) {
	out := hourlyBuckets{
		requests: zeroSparkline(),
		errors:   zeroSparkline(),
		cost:     zeroSparkline(),
	}
	if s.db == nil || s.db.Pool == nil {
		return out, nil
	}
	start := now.Add(-24 * time.Hour).Truncate(time.Hour)
	baseHour := now.Truncate(time.Hour).Add(-23 * time.Hour)

	// Requests + errors.
	rows, err := s.db.Pool.Query(ctx,
		"select date_trunc('hour', created_at) as bucket, "+
			"count(*), "+
			"count(*) filter (where status_code is null or status_code < 200 or status_code >= 300) "+
			"from suite_gateway_requests "+
			"where created_at >= $1 "+
			"group by bucket "+
			"order by bucket",
		start)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			bucket time.Time
			total  int64
			failed int64
		)
		if err := rows.Scan(&bucket, &total, &failed); err != nil {
			return out, err
		}
		idx := int(bucket.Sub(baseHour).Hours())
		if idx < 0 || idx >= 24 {
			continue
		}
		out.requests[idx] = float64(total)
		out.errors[idx] = float64(failed)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	// Cost. Separate query so an empty cost_events table doesn't
	// hide gateway data via inner-join semantics.
	costRows, err := s.db.Pool.Query(ctx,
		"select date_trunc('hour', occurred_at) as bucket, "+
			"coalesce(sum(cost_usd), 0) "+
			"from suite_cost_events "+
			"where occurred_at >= $1 "+
			"group by bucket "+
			"order by bucket",
		start)
	if err != nil {
		return out, err
	}
	defer costRows.Close()
	for costRows.Next() {
		var (
			bucket time.Time
			cost   float64
		)
		if err := costRows.Scan(&bucket, &cost); err != nil {
			return out, err
		}
		idx := int(bucket.Sub(baseHour).Hours())
		if idx < 0 || idx >= 24 {
			continue
		}
		out.cost[idx] = cost
	}
	return out, costRows.Err()
}

func zeroSparkline() []float64 {
	return make([]float64, 24)
}

// ─── GET /api/v1/cost ─────────────────────────────────────────────────────

// costSummaryResponse matches CostSummarySchema.
type costSummaryResponse struct {
	PeriodTotalUSD   float64        `json:"period_total_usd"`
	PreviousTotalUSD float64        `json:"previous_total_usd"`
	BudgetUSD        *float64       `json:"budget_usd"` // nullable per schema
	ForecastUSD      float64        `json:"forecast_usd"`
	ByDay            []costPoint    `json:"by_day"`
	ByModel          []costByModel  `json:"by_model"`
	ByAgent          []costByAgent  `json:"by_agent"`
	ByTenant         []costByTenant `json:"by_tenant"`
}

type costPoint struct {
	Date    string  `json:"date"`
	CostUSD float64 `json:"cost_usd"`
}

type costByModel struct {
	Model   string  `json:"model"`
	CostUSD float64 `json:"cost_usd"`
}

type costByAgent struct {
	Agent   string  `json:"agent"`
	CostUSD float64 `json:"cost_usd"`
}

type costByTenant struct {
	TenantID   string  `json:"tenant_id"`
	TenantName *string `json:"tenant_name"` // nullable per schema
	CostUSD    float64 `json:"cost_usd"`
}

// handleCostSummary returns the dashboard's cost summary panel.
//
// Backed by the cost.Aggregate package (Phase 7). When no DB / no
// Aggregate is wired (boot mode), responds with empty/zero panels so
// the dashboard still renders skeletons rather than erroring.
//
// Query params:
//
//	from    RFC3339 — defaults to start of current month UTC
//	to      RFC3339 — defaults to now UTC
//	tenant  uuid     — scopes the summary to a single tenant (operator
//	                   view by default reports across tenants)
func (s *Server) handleCostSummary(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.cost.summary")
	defer span.End()

	resp := costSummaryResponse{
		PeriodTotalUSD:   0,
		PreviousTotalUSD: 0,
		BudgetUSD:        nil, // explicit JSON null — schema field is nullable
		ForecastUSD:      0,
		ByDay:            []costPoint{},
		ByModel:          []costByModel{},
		ByAgent:          []costByAgent{},
		ByTenant:         []costByTenant{},
	}

	if s.costAgg == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	q := r.URL.Query()
	opts := cost.AggregateOpts{
		TenantID: strings.TrimSpace(q.Get("tenant")),
	}
	if v := strings.TrimSpace(q.Get("from")); v != "" {
		t, err := parseRFC3339(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "from must be RFC3339", nil)
			return
		}
		opts.PeriodStart = t
	}
	if v := strings.TrimSpace(q.Get("to")); v != "" {
		t, err := parseRFC3339(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "to must be RFC3339", nil)
			return
		}
		opts.PeriodEnd = t
	}

	summary, err := s.costAgg.Summary(ctx, opts)
	if err != nil {
		span.RecordError(err)
		s.log.Warn("cost summary failed", "error", err)
		// Graceful degrade: dashboard still gets a valid envelope.
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.PeriodTotalUSD = summary.PeriodTotalUSD
	resp.PreviousTotalUSD = summary.PreviousTotalUSD
	resp.BudgetUSD = summary.BudgetUSD
	resp.ForecastUSD = summary.ForecastUSD
	for _, d := range summary.ByDay {
		resp.ByDay = append(resp.ByDay, costPoint{Date: d.Date, CostUSD: d.CostUSD})
	}
	for _, m := range summary.ByModel {
		resp.ByModel = append(resp.ByModel, costByModel{Model: m.Model, CostUSD: m.CostUSD})
	}
	for _, a := range summary.ByAgent {
		resp.ByAgent = append(resp.ByAgent, costByAgent{Agent: a.Agent, CostUSD: a.CostUSD})
	}
	for _, t := range summary.ByTenant {
		resp.ByTenant = append(resp.ByTenant, costByTenant{
			TenantID:   t.TenantID,
			TenantName: t.TenantName,
			CostUSD:    t.CostUSD,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── GET /api/v1/modules ──────────────────────────────────────────────────

// moduleEntry matches one element of ModulesStateSchema.modules.
type moduleEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Adapter string `json:"adapter,omitempty"`
	Version string `json:"version,omitempty"`
}

// modulesStateResponse matches ModulesStateSchema.
type modulesStateResponse struct {
	Modules             []moduleEntry `json:"modules"`
	WorkloadModules     []string      `json:"workload_modules"`
	MultiTenancyEnabled bool          `json:"multi_tenancy_enabled"`
}

// v1ModuleCatalogue is the canonical list of core suite modules for v1.
// Each entry's defaultEnabled value applies when config.yaml doesn't
// pin the module explicitly. multi-tenancy and sandbox default OFF; the
// rest default ON.
type catalogueEntry struct {
	ID             string
	Name           string
	DefaultEnabled bool
}

var v1ModuleCatalogue = []catalogueEntry{
	{ID: "identity", Name: "Identity"},
	{ID: "public-gateway", Name: "Public Gateway"},
	{ID: "llm-gateway", Name: "LLM Gateway"},
	{ID: "jobs", Name: "Jobs"},
	{ID: "secrets-vault", Name: "Secrets Vault"},
	{ID: "storage", Name: "Storage"},
	{ID: "notifications", Name: "Notifications"},
	{ID: "webhooks-in", Name: "Webhooks (Inbound)"},
	{ID: "billing", Name: "Billing"},
	{ID: "observability", Name: "Observability"},
	{ID: "mcp-client", Name: "MCP Client"},
	{ID: "multi-tenancy", Name: "Multi-tenancy"},
	{ID: "sandbox", Name: "Sandbox"},
}

func init() {
	// Enabled-by-default for everything except multi-tenancy and sandbox.
	for i := range v1ModuleCatalogue {
		switch v1ModuleCatalogue[i].ID {
		case "multi-tenancy", "sandbox":
			v1ModuleCatalogue[i].DefaultEnabled = false
		default:
			v1ModuleCatalogue[i].DefaultEnabled = true
		}
	}
}

// handleModulesState describes which suite modules are enabled in this
// runtime. Read by the dashboard at boot to decide which tabs/pages to
// render and whether to gate multi-tenancy features.
func (s *Server) handleModulesState(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.modules.state")
	defer span.End()

	cfgEnabled := s.cfg.Modules.Enabled
	cfgAdapters := s.cfg.Modules.Adapters

	entries := make([]moduleEntry, 0, len(v1ModuleCatalogue))
	mtEnabled := false
	for _, c := range v1ModuleCatalogue {
		on := c.DefaultEnabled
		if v, ok := cfgEnabled[c.ID]; ok {
			on = v
		}
		entry := moduleEntry{ID: c.ID, Name: c.Name, Enabled: on}
		if a, ok := cfgAdapters[c.ID]; ok && a != "" {
			entry.Adapter = a
		}
		entries = append(entries, entry)
		if c.ID == "multi-tenancy" {
			mtEnabled = on
		}
	}

	workload := s.cfg.Modules.WorkloadModules
	if workload == nil {
		workload = []string{}
	}

	resp := modulesStateResponse{
		Modules:             entries,
		WorkloadModules:     workload,
		MultiTenancyEnabled: mtEnabled,
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── GET /api/v1/queues/summary ───────────────────────────────────────────

// queueSummaryResponse matches QueueSummarySchema.
type queueSummaryResponse struct {
	Pending        int          `json:"pending"`
	Running        int          `json:"running"`
	Failed         int          `json:"failed"`
	SucceededToday int          `json:"succeeded_today"`
	Recent         []queueEntry `json:"recent"`
}

type queueEntry struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	EnqueuedAt string  `json:"enqueued_at"`
	Attempts   int     `json:"attempts"`
	LastError  *string `json:"last_error"` // nullable per schema
}

// handleQueueSummary returns the jobs queue summary backed by River.
//
// Reads the live counts (pending/running/failed/succeeded_today) and the
// most recent N rows from the jobs Manager. When the Manager isn't wired
// (no DB / Phase 4 boot) we return zeros + an empty list so the dashboard
// renders skeletons rather than errors.
func (s *Server) handleQueueSummary(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.queues.summary")
	defer span.End()

	resp := queueSummaryResponse{Recent: []queueEntry{}}
	if s.jobs == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	summary, err := s.jobs.Summary(ctx, 50)
	if err != nil {
		span.RecordError(err)
		s.log.Warn("queue summary failed", "error", err)
		// Still return zeros so the dashboard doesn't error out.
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Pending = summary.Pending
	resp.Running = summary.Running
	resp.Failed = summary.Failed
	resp.SucceededToday = summary.SucceededToday
	for _, row := range summary.Recent {
		entry := queueEntry{
			ID:         strconv.FormatInt(row.ID, 10),
			Name:       row.Kind,
			Status:     mapJobStateForSummary(string(row.State)),
			EnqueuedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano),
			Attempts:   row.Attempt,
		}
		if len(row.Errors) > 0 {
			msg := row.Errors[len(row.Errors)-1].Error
			entry.LastError = &msg
		}
		resp.Recent = append(resp.Recent, entry)
	}
	writeJSON(w, http.StatusOK, resp)
}

// mapJobStateForSummary maps a River JobState to the smaller status enum
// used by the queue summary schema (which only has 5 values).
func mapJobStateForSummary(state string) string {
	switch state {
	case "available", "scheduled", "retryable", "pending":
		return "pending"
	case "running":
		return "running"
	case "completed":
		return "succeeded"
	case "discarded":
		return "failed"
	case "cancelled":
		return "cancelled"
	default:
		return "pending"
	}
}

// ─── Home overview helpers (Gaps 3, 4 + webhook deliveries) ───────────────

// fetchRecentWebhookDeliveries reads the most-recent N webhook delivery rows
// across all tenants and maps each to the Home overview's narrower vocabulary
// (HomeOverviewSchema in apps/dashboard/src/lib/api.ts).
//
// Direction:
//
//	"inbound"  -> "in"
//	"outbound" -> "out"
//
// Status:
//
//	"succeeded"             -> "delivered"
//	"failed"                -> "failed"
//	anything else (queued,
//	   retrying, dropped*)  -> "pending"
//
// The URL field falls back to suite_webhook_deliveries.destination, which
// is set for outbound deliveries; for inbound rows it's the endpoint we
// received from. Both are useful for activity feed rendering.
func (s *Server) fetchRecentWebhookDeliveries(ctx context.Context, limit int) ([]webhookDelivery, error) {
	out := []webhookDelivery{}
	if s.db == nil || s.db.Pool == nil {
		return out, nil
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.Pool.Query(ctx, `
		select id::text, destination, direction, status, created_at
		  from suite_webhook_deliveries
		 order by created_at desc
		 limit $1
	`, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id          string
			destination string
			direction   string
			status      string
			createdAt   time.Time
		)
		if err := rows.Scan(&id, &destination, &direction, &status, &createdAt); err != nil {
			return out, err
		}
		out = append(out, webhookDelivery{
			ID:         id,
			URL:        destination,
			Direction:  mapWebhookDirection(direction),
			Status:     mapWebhookStatus(status),
			OccurredAt: createdAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return out, rows.Err()
}

// mapWebhookDirection collapses suite_webhook_deliveries.direction into the
// short enum the dashboard schema expects.
func mapWebhookDirection(s string) string {
	if s == "inbound" {
		return "in"
	}
	return "out"
}

// mapWebhookStatus collapses suite_webhook_deliveries.status into the short
// enum the dashboard schema expects. Unknown / in-flight values fall back to
// "pending" so the activity feed always has a renderable badge.
func mapWebhookStatus(s string) string {
	switch s {
	case "succeeded":
		return "delivered"
	case "failed":
		return "failed"
	default:
		return "pending"
	}
}

// computeBudgetsAggregate folds every per-tenant budget into the scalar
// summary the Home strip's "Budget consumed %" tile renders. "At risk" is
// the canonical condition the gateway uses elsewhere — once a tenant's
// consumed_pct crosses their configured alert_threshold_pct they show up in
// this count.
func computeBudgetsAggregate(ctx context.Context, budgets *cost.Budgets) (budgetsAggregate, error) {
	agg := budgetsAggregate{}
	list, err := budgets.List(ctx)
	if err != nil {
		return agg, err
	}
	agg.TenantCount = len(list)
	if len(list) == 0 {
		return agg, nil
	}
	var totalPct float64
	for _, b := range list {
		if b.MonthlyUSD <= 0 {
			continue
		}
		pct := (b.SpentThisPeriodUSD / b.MonthlyUSD) * 100
		totalPct += pct
		if pct >= float64(b.AlertThresholdPct) {
			agg.TenantsAtRisk++
		}
	}
	agg.AvgConsumedPct = totalPct / float64(len(list))
	return agg, nil
}

// sparklineDeltaPct returns the percent change between the average of the
// last 4 buckets and the average of buckets [-8:-4]. Used to render the
// "▲ 12%" annotation next to each KPI value on the Home strip.
//
// Returns 0 (not NaN, not Inf) when either window has no samples or the
// prior average is zero — the dashboard expects a renderable number always.
func sparklineDeltaPct(buckets []float64) float64 {
	if len(buckets) < 8 {
		return 0
	}
	recent := buckets[len(buckets)-4:]
	prior := buckets[len(buckets)-8 : len(buckets)-4]
	var ra, pa float64
	for _, v := range recent {
		ra += v
	}
	for _, v := range prior {
		pa += v
	}
	ra /= float64(len(recent))
	pa /= float64(len(prior))
	if pa == 0 {
		return 0
	}
	return ((ra - pa) / pa) * 100
}

// ─── Compile-time safety net ──────────────────────────────────────────────

// Ensure pgtype is referenced even if a future refactor drops UUID
// scanning so that go.mod doesn't lose the dependency silently.
var _ = errors.New