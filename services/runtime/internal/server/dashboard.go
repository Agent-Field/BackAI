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

	// Build the WHERE clause. We always filter to gateway-request rows
	// that targeted an agent execute endpoint, so this view is "agent
	// runs" rather than every HTTP hit the gateway recorded.
	conds := []string{"endpoint like '/api/v1/execute/%'"}
	args := []any{}
	bind := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if q.Agent != "" {
		// Match both sync and async endpoint variants for the requested
		// agent call (e.g. "/api/v1/execute/sample.echo" and
		// "/api/v1/execute/async/sample.echo").
		syncBind := bind("/api/v1/execute/" + q.Agent)
		asyncBind := bind("/api/v1/execute/async/" + q.Agent)
		conds = append(conds, "(endpoint = "+syncBind+" or endpoint = "+asyncBind+")")
	}
	if q.Tenant != "" {
		conds = append(conds, "tenant_id::text = "+bind(q.Tenant))
	}
	switch q.Status {
	case "succeeded":
		conds = append(conds, "status_code >= 200 and status_code < 300")
	case "failed":
		conds = append(conds, "(status_code is null or status_code < 200 or status_code >= 300)")
	}
	where := "where " + strings.Join(conds, " and ")

	// Total count for pagination.
	var total int64
	countSQL := "select count(*) from suite_gateway_requests " + where
	if err := s.db.Pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return out, err
	}
	out.Total = total

	// Page of rows.
	pageArgs := append([]any{}, args...)
	pageArgs = append(pageArgs, q.Limit, q.Offset)
	limitIdx := len(pageArgs) - 1
	offsetIdx := len(pageArgs)
	rowsSQL := "select id, coalesce(tenant_id::text, ''), endpoint, status_code, duration_ms, created_at " +
		"from suite_gateway_requests " + where +
		" order by created_at desc limit $" + strconv.Itoa(limitIdx) +
		" offset $" + strconv.Itoa(offsetIdx)

	rows, err := s.db.Pool.Query(ctx, rowsSQL, pageArgs...)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id         pgtype.UUID
			tenantID   string
			endpoint   string
			statusCode pgtype.Int4
			durationMS pgtype.Int4
			createdAt  time.Time
		)
		if err := rows.Scan(&id, &tenantID, &endpoint, &statusCode, &durationMS, &createdAt); err != nil {
			return out, err
		}
		rec := runRecord{
			ID:        uuidString(id),
			Agent:     agentFromEndpoint(endpoint),
			Status:    classifyStatus(statusCode),
			StartedAt: createdAt.UTC().Format(time.RFC3339Nano),
			CostUSD:   0, // TODO Phase 7: wire real cost
		}
		if tenantID != "" {
			rec.TenantID = tenantID
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

// agentFromEndpoint strips the /api/v1/execute/ (or async) prefix to
// recover the agent call name (e.g. "sample.echo"). Returns the raw
// endpoint if the prefix is missing.
func agentFromEndpoint(endpoint string) string {
	const sync = "/api/v1/execute/"
	const async = "/api/v1/execute/async/"
	if strings.HasPrefix(endpoint, async) {
		return strings.TrimPrefix(endpoint, async)
	}
	if strings.HasPrefix(endpoint, sync) {
		return strings.TrimPrefix(endpoint, sync)
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
	RequestsPerMinute       float64                  `json:"requests_per_minute"`
	ErrorRate               float64                  `json:"error_rate"`
	CostTodayUSD            float64                  `json:"cost_today_usd"`
	QueueDepth              int                      `json:"queue_depth"`
	RequestSparkline        []float64                `json:"request_sparkline"`
	ErrorSparkline          []float64                `json:"error_sparkline"`
	CostSparkline           []float64                `json:"cost_sparkline"`
	QueueSparkline          []float64                `json:"queue_sparkline"`
	RecentRuns              []runRecord              `json:"recent_runs"`
	RecentWebhookDeliveries []webhookDelivery        `json:"recent_webhook_deliveries"`
	Alerts                  []dashboardAlert         `json:"alerts"`
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

		// cost_today_usd is 0 in Phase 4 — we don't track cost yet.
		// TODO Phase 7: sum cost_usd column once it lands on
		// suite_gateway_requests (or a sibling cost ledger).
		resp.CostTodayUSD = 0

		// Sparklines: bucket the last 24 hours into one-hour windows.
		if buckets, err := s.fetchHourlyBuckets(ctx, now); err == nil {
			resp.RequestSparkline = buckets.requests
			resp.ErrorSparkline = buckets.errors
			// cost sparkline stays zero — see TODO above.
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
}

// fetchHourlyBuckets returns 24-element arrays where index 0 is 23 hours
// ago and index 23 is the current hour. PG aggregates everything in one
// round-trip using date_trunc('hour').
func (s *Server) fetchHourlyBuckets(ctx context.Context, now time.Time) (hourlyBuckets, error) {
	out := hourlyBuckets{
		requests: zeroSparkline(),
		errors:   zeroSparkline(),
	}
	if s.db == nil || s.db.Pool == nil {
		return out, nil
	}
	start := now.Add(-24 * time.Hour).Truncate(time.Hour)
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

	baseHour := now.Truncate(time.Hour).Add(-23 * time.Hour)
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
	return out, rows.Err()
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

// handleCostSummary returns a stub cost summary. Phase 4 doesn't track
// cost yet — Phase 7 wires the LLM gateway's per-call cost into a sibling
// `suite_cost_events` table. Schema MUST still validate, so we return the
// nullable budget as JSON null (Go nil pointer) and empty arrays for the
// breakdowns.
//
// Query params `from` and `to` are accepted but ignored until Phase 7.
func (s *Server) handleCostSummary(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.cost.summary")
	defer span.End()

	// TODO Phase 7: read from suite_cost_events keyed on tenant_id, model,
	// agent, and bucket by day across [from,to]. For now: zeros + empty.
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

// ─── Compile-time safety net ──────────────────────────────────────────────

// Ensure pgtype is referenced even if a future refactor drops UUID
// scanning so that go.mod doesn't lose the dependency silently.
var _ = errors.New
