// SPDX-License-Identifier: Apache-2.0

// Package cost implements per-call LLM cost tracking + per-tenant budget
// enforcement for AF Stack Phase 7.
//
// Architecture
//
//  1. The LLM gateway (Phase 7.1) fires hooks.HookLLMPreCall before each
//     upstream call and hooks.HookLLMPostCall after. Recorder/Budgets in
//     this package supply the handlers wired in main.go.
//  2. Pre-call: Budgets.HasBudget checks the current period's spend vs the
//     tenant's monthly_usd cap. A nil budget means "no cap" — calls pass
//     through. An exhausted budget returns ErrBudgetExceeded, which the
//     gateway translates to HTTP 402.
//  3. Post-call: Recorder.Record writes one row to suite_cost_events.
//     Best-effort by design — a write failure logs but never errors the
//     LLM call (we'd rather lose a ledger row than fail a request).
//
// # Aggregation
//
// Aggregate.Summary powers the dashboard's /api/v1/cost summary card and
// per-breakdown tables. Aggregate.Events powers /api/v1/cost/events for
// the detailed event log. Forecast is linear extrapolation from the
// elapsed fraction of the current period (current_period_total /
// elapsed_fraction).
//
// # RLS note
//
// suite_cost_events and suite_budgets are intentionally NOT RLS-gated.
// Phase 6 (migration 00004) excluded admin-managed tables from RLS
// because the dashboard's /api/v1/cost summary, /api/v1/admin/budgets
// list, and operator-mode "cost by tenant" view all need cross-tenant
// visibility. Tenant scoping happens at the query layer (WHERE
// tenant_id = $1) rather than via the app.tenant_id session GUC.
package cost

import (
	"errors"
	"time"
)

// Event is the data the LLM gateway emits per call. The Recorder writes
// this to suite_cost_events; the schema matches CostEventSchema in
// apps/dashboard/src/lib/api.ts (snake_case JSON tags applied at the
// HTTP layer).
type Event struct {
	// RequestID is the gateway request id. It lets customer apps link
	// a user-visible action to the exact operator cost event.
	RequestID string
	// TenantID is the owning tenant. Empty means "no tenant" (the row
	// is written with NULL tenant_id — the FK is set null).
	TenantID string
	// APIKeyID is the suite_api_keys.id the request authenticated as.
	// Empty -> NULL in the row.
	APIKeyID string
	// Model is the canonical model id routed by the gateway (e.g.
	// "openrouter/qwen/qwen-2.5-72b-instruct"). Used by pricing.Lookup.
	Model string
	// Provider is the upstream provider id (anthropic, openai, etc.).
	// Carried separately from model so a future model migration (id
	// rename) doesn't lose the provider breakdown.
	Provider string
	// Agent is the AF Stack agent id (e.g. "sample.echo") that issued
	// the call. Empty when the call came from the raw LLM gateway path
	// rather than via an agent invocation.
	Agent string
	// Reasoner is the leaf reasoner id captured from X-AF-Reasoner.
	// Empty for raw gateway calls and older SDKs that do not send it.
	Reasoner string
	// PromptTokens / CompletionTokens are the provider's reported
	// usage. TotalTokens is derived (prompt + completion) but stored
	// because some providers report total separately from sums.
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	// CostUSD is the dollar cost. The gateway computes this via
	// pricing.EstimateCostUSD before calling Record. Stored at numeric(12,6)
	// precision in PG; rounding happens at the dashboard layer.
	CostUSD float64
	// Cached is true when the call was served from the LLM response
	// cache (Phase 7.3); cost_usd is then 0 but the call is still
	// recorded so the cache hit rate is computable.
	Cached bool
	// LatencyMS is end-to-end gateway-observed latency.
	LatencyMS int
	// StatusCode and ErrorCode capture terminal gateway status for
	// reasoner analytics. ErrorCode is empty on success.
	StatusCode int
	ErrorCode  string
	// OccurredAt is the call timestamp. Zero -> the recorder uses now().
	OccurredAt time.Time
	// Modality classifies the call: "text" (default for chat), "embedding",
	// "audio_speech" (TTS), "audio_transcription" (STT),
	// "audio_translation" (whisper translate), "image", "video". The
	// dashboard renders a "cost by modality" stack from this column.
	Modality string
}

// Budget is the per-tenant monthly cap. Mirrors BudgetSchema in
// apps/dashboard/src/lib/api.ts.
type Budget struct {
	// TenantID is the tenant the budget applies to (primary key in
	// suite_budgets — at most one budget per tenant).
	TenantID string
	// MonthlyUSD is the hard cap. The gateway rejects new LLM calls
	// once SpentThisPeriodUSD + estimate > MonthlyUSD.
	MonthlyUSD float64
	// AlertThresholdPct is the operator's alarm point (0..100). The
	// dashboard renders an orange badge once spent/monthly crosses
	// this fraction. Pure cosmetic — does not gate any logic.
	AlertThresholdPct int
	// SpentThisPeriodUSD is the actual ledger sum for the current
	// period [CurrentPeriodStart, CurrentPeriodStart + 1 month).
	// Computed live by Get/List from suite_cost_events.
	SpentThisPeriodUSD float64
	// RemainingUSD is MonthlyUSD - SpentThisPeriodUSD, floored at 0.
	RemainingUSD float64
	// ResetsAt is the start of the next period (CurrentPeriodStart
	// rolled forward by one calendar month). The dashboard uses this
	// to show "resets in 11 days".
	ResetsAt time.Time
	// CurrentPeriodStart is the bound where Spent is computed from.
	CurrentPeriodStart time.Time
}

// SetBudgetInput is the body of PUT /api/v1/admin/budgets. Mirrors
// SetBudgetInputSchema. AlertThresholdPct defaults to 80 at the HTTP
// layer when omitted (schema default).
type SetBudgetInput struct {
	TenantID          string
	MonthlyUSD        float64
	AlertThresholdPct int
}

// AggregateOpts scopes a Summary call.
type AggregateOpts struct {
	// TenantID, when non-empty, scopes the summary to a single tenant.
	// Empty = cross-tenant operator view.
	TenantID string
	// PeriodStart / PeriodEnd bound the current period. Zero values
	// default to the start of the current calendar month and "now".
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// EventsOpts scopes an Events list (paginated).
type EventsOpts struct {
	TenantID  string
	RequestID string
	Model     string
	From      time.Time
	To        time.Time
	Limit     int
	Offset    int
}

// Sentinel errors -----------------------------------------------------------

// ErrBudgetExceeded is returned by Budgets.HasBudget when the caller
// would push the tenant's period spend over MonthlyUSD. The LLM
// gateway hook handler translates this into a Pre-call abort; the
// gateway then returns HTTP 402 to the client.
var ErrBudgetExceeded = errors.New("cost: budget exceeded for tenant")

// ErrBudgetNotFound is returned by Budgets.Get when the tenant has no
// budget row. The admin handler maps this to HTTP 404.
var ErrBudgetNotFound = errors.New("cost: no budget set for tenant")

// ErrInvalidInput is returned for malformed SetBudgetInput. The admin
// handler maps this to HTTP 400.
var ErrInvalidInput = errors.New("cost: invalid input")
