// SPDX-License-Identifier: Apache-2.0

// cost.go — REST handlers for the Phase 7 cost ledger + budgets.
//
// Endpoints (paths + response shapes match apps/dashboard/src/lib/api.ts):
//
//   GET    /api/v1/cost/events             -> CostEventList
//   GET    /api/v1/admin/budgets           -> BudgetList
//   GET    /api/v1/admin/budgets/{tenantId} -> Budget
//   PUT    /api/v1/admin/budgets           -> Budget   (body: SetBudgetInput)
//
// Admin endpoints are gated by the multi-tenancy module flag (consistent
// with Phase 6: per-tenant administration requires MT). The cost events
// list endpoint is NOT gated — the operator-mode dashboard renders it
// even on single-tenant deployments.

package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Agent-Field/backai/services/runtime/internal/cost"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

// ─── Wire shapes ──────────────────────────────────────────────────────────

// costEventWire mirrors CostEventSchema (apps/dashboard/src/lib/api.ts).
// Nullable fields use *string so they serialise as JSON null when absent.
type costEventWire struct {
	ID               string  `json:"id"`
	TenantID         *string `json:"tenant_id"`
	APIKeyID         *string `json:"api_key_id"`
	Model            string  `json:"model"`
	Provider         string  `json:"provider"`
	Agent            *string `json:"agent"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	Cached           bool    `json:"cached"`
	LatencyMS        int     `json:"latency_ms"`
	OccurredAt       string  `json:"occurred_at"`
}

// costEventListWire mirrors CostEventListSchema.
type costEventListWire struct {
	Events  []costEventWire `json:"events"`
	Total   int64           `json:"total"`
	HasMore bool            `json:"has_more"`
}

// budgetWire mirrors BudgetSchema.
type budgetWire struct {
	TenantID           string  `json:"tenant_id"`
	MonthlyUSD         float64 `json:"monthly_usd"`
	AlertThresholdPct  int     `json:"alert_threshold_pct"`
	SpentThisPeriodUSD float64 `json:"spent_this_period_usd"`
	RemainingUSD       float64 `json:"remaining_usd"`
	ResetsAt           string  `json:"resets_at"`
}

// budgetListWire mirrors BudgetListSchema.
type budgetListWire struct {
	Budgets []budgetWire `json:"budgets"`
}

// setBudgetInputWire mirrors SetBudgetInputSchema.
type setBudgetInputWire struct {
	TenantID          string  `json:"tenant_id"`
	MonthlyUSD        float64 `json:"monthly_usd"`
	AlertThresholdPct int     `json:"alert_threshold_pct"`
}

// ─── Marshallers ──────────────────────────────────────────────────────────

func marshalCostEvent(e cost.EventRow) costEventWire {
	return costEventWire{
		ID:               e.ID,
		TenantID:         e.TenantID,
		APIKeyID:         e.APIKeyID,
		Model:            e.Model,
		Provider:         e.Provider,
		Agent:            e.Agent,
		PromptTokens:     e.PromptTokens,
		CompletionTokens: e.CompletionTokens,
		TotalTokens:      e.TotalTokens,
		CostUSD:          e.CostUSD,
		Cached:           e.Cached,
		LatencyMS:        e.LatencyMS,
		OccurredAt:       e.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

func marshalBudget(b cost.Budget) budgetWire {
	return budgetWire{
		TenantID:           b.TenantID,
		MonthlyUSD:         b.MonthlyUSD,
		AlertThresholdPct:  b.AlertThresholdPct,
		SpentThisPeriodUSD: b.SpentThisPeriodUSD,
		RemainingUSD:       b.RemainingUSD,
		ResetsAt:           b.ResetsAt.UTC().Format(time.RFC3339Nano),
	}
}

// ─── Route registration ───────────────────────────────────────────────────

// registerCostRoutes wires the cost ledger + budget endpoints.
func (s *Server) registerCostRoutes() {
	s.mux.HandleFunc("GET /api/v1/cost/events", s.handleListCostEvents)
	s.mux.HandleFunc("GET /api/v1/admin/budgets", s.handleAdminListBudgets)
	s.mux.HandleFunc("GET /api/v1/admin/budgets/{tenantId}", s.handleAdminGetBudget)
	s.mux.HandleFunc("PUT /api/v1/admin/budgets", s.handleAdminSetBudget)
}

func (s *Server) registerCostOpenAPI() {
	if s.openapi == nil {
		return
	}
	b := s.openapi
	dashTags := []string{"dashboard"}
	adminTags := []string{"admin"}

	b.Register("GET", "/api/v1/cost/events", openapi.RouteMeta{
		Summary: "List per-call LLM cost events", Tags: dashTags,
	})
	b.Register("GET", "/api/v1/admin/budgets", openapi.RouteMeta{
		Summary: "List per-tenant monthly budgets", Tags: adminTags,
	})
	b.Register("GET", "/api/v1/admin/budgets/{tenantId}", openapi.RouteMeta{
		Summary: "Get a tenant's monthly budget", Tags: adminTags,
		Parameters: []openapi.Parameter{
			{Name: "tenantId", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("PUT", "/api/v1/admin/budgets", openapi.RouteMeta{
		Summary: "Set (upsert) a tenant's monthly budget", Tags: adminTags,
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// costEventsUnavailable returns true (and writes a 503) when no
// Aggregate is configured. The list endpoint returns empty data when
// pool is configured but empty; "service unavailable" is reserved for
// the no-Aggregate boot path.
func (s *Server) costEventsUnavailable(w http.ResponseWriter) bool {
	if s.costAgg == nil {
		writeError(w, http.StatusServiceUnavailable,
			"COST_NOT_CONFIGURED",
			"cost aggregate is not configured on this runtime",
			nil)
		return true
	}
	return false
}

// budgetsUnavailable enforces both the MT gate and the configured-store
// requirement. Returns true (and writes the appropriate 503 envelope)
// if the caller should NOT proceed.
func (s *Server) budgetsUnavailable(w http.ResponseWriter) bool {
	if !s.multiTenancyEnabled() {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("MT_DISABLED", "multi-tenancy module not enabled"))
		return true
	}
	if s.budgets == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("BUDGETS_NOT_CONFIGURED",
				"budgets store is not configured on this runtime"))
		return true
	}
	return false
}

// writeBudgetError maps cost-package sentinels to HTTP envelopes.
func writeBudgetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cost.ErrBudgetNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error(), nil)
	case errors.Is(err, cost.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", "budget operation failed", nil)
	}
}

// ─── Handlers ─────────────────────────────────────────────────────────────

// handleListCostEvents implements GET /api/v1/cost/events.
//
// Query params:
//
//	tenant uuid          — scope to tenant
//	model  string        — scope to model id
//	from   RFC3339       — lower bound (inclusive)
//	to     RFC3339       — upper bound (exclusive)
//	limit  int (1..200)  — page size (default 50)
//	offset int (>=0)     — page offset
func (s *Server) handleListCostEvents(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "cost.events.list")
	defer span.End()
	if s.costEventsUnavailable(w) {
		return
	}

	q := r.URL.Query()
	limit, offset := parsePaging(q.Get("limit"), q.Get("offset"))
	opts := cost.EventsOpts{
		TenantID: strings.TrimSpace(q.Get("tenant")),
		Model:    strings.TrimSpace(q.Get("model")),
		Limit:    limit,
		Offset:   offset,
	}
	if v := strings.TrimSpace(q.Get("from")); v != "" {
		t, err := parseRFC3339(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "from must be RFC3339", nil)
			return
		}
		opts.From = t
	}
	if v := strings.TrimSpace(q.Get("to")); v != "" {
		t, err := parseRFC3339(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "to must be RFC3339", nil)
			return
		}
		opts.To = t
	}
	span.SetAttributes(
		attribute.String("filter.tenant", opts.TenantID),
		attribute.String("filter.model", opts.Model),
		attribute.Int("paging.limit", limit),
		attribute.Int("paging.offset", offset),
	)

	page, err := s.costAgg.Events(ctx, opts)
	if err != nil {
		span.RecordError(err)
		s.log.Error("cost events list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}

	wireList := make([]costEventWire, 0, len(page.Events))
	for _, e := range page.Events {
		wireList = append(wireList, marshalCostEvent(e))
	}
	writeJSON(w, http.StatusOK, costEventListWire{
		Events:  wireList,
		Total:   page.Total,
		HasMore: page.HasMore,
	})
}

// handleAdminListBudgets implements GET /api/v1/admin/budgets.
func (s *Server) handleAdminListBudgets(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.budgets.list")
	defer span.End()
	if s.budgetsUnavailable(w) {
		return
	}
	rows, err := s.budgets.List(ctx)
	if err != nil {
		span.RecordError(err)
		s.log.Error("budget list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	out := make([]budgetWire, 0, len(rows))
	for _, b := range rows {
		out = append(out, marshalBudget(b))
	}
	writeJSON(w, http.StatusOK, budgetListWire{Budgets: out})
}

// handleAdminGetBudget implements GET /api/v1/admin/budgets/{tenantId}.
func (s *Server) handleAdminGetBudget(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.budgets.get")
	defer span.End()
	if s.budgetsUnavailable(w) {
		return
	}
	tenantID := r.PathValue("tenantId")
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "tenantId is required", nil)
		return
	}
	span.SetAttributes(attribute.String("tenant.id", tenantID))
	b, err := s.budgets.Get(ctx, tenantID)
	if err != nil {
		span.RecordError(err)
		writeBudgetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, marshalBudget(b))
}

// handleAdminSetBudget implements PUT /api/v1/admin/budgets.
//
// Upsert keyed on tenant_id. monthly_usd must be positive;
// alert_threshold_pct defaults to 80 when omitted.
func (s *Server) handleAdminSetBudget(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.budgets.set")
	defer span.End()
	if s.budgetsUnavailable(w) {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "request body is required", nil)
		return
	}
	var in setBudgetInputWire
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}

	// Schema default for AlertThresholdPct is 80 when omitted.
	thresh := in.AlertThresholdPct
	if thresh == 0 {
		thresh = 80
	}
	span.SetAttributes(
		attribute.String("tenant.id", in.TenantID),
		attribute.Float64("budget.monthly_usd", in.MonthlyUSD),
		attribute.Int("budget.alert_threshold_pct", thresh),
	)

	b, err := s.budgets.Set(ctx, cost.SetBudgetInput{
		TenantID:          in.TenantID,
		MonthlyUSD:        in.MonthlyUSD,
		AlertThresholdPct: thresh,
	})
	if err != nil {
		span.RecordError(err)
		writeBudgetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, marshalBudget(b))
}