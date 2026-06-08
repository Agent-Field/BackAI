// SPDX-License-Identifier: Apache-2.0

// recorder.go — best-effort audit writer for LLM cost events.
//
// One Recorder is constructed per process and shared with the
// hooks.HookLLMPostCall handler. Record() inserts a single row into
// suite_cost_events. The write is best-effort: a failure logs but never
// errors the LLM call — losing a row is preferable to failing a
// user-visible request.
//
// SOURCE OF TRUTH (item #22): cumulative spend + budget remaining live
// in LiteLLM. suite_cost_events is a write-through AUDIT TABLE — it
// preserves per-event detail (modality, agent, cached, latency) that
// LiteLLM doesn't surface, and powers the operator's "event log" view.
// The dashboard reads canonical totals from cost.Aggregate.FromLiteLLM,
// not from a sum over this table.

package cost

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Recorder writes Event rows to suite_cost_events.
//
// The Pool may be nil — in that case Record() logs and returns nil so
// the LLM gateway boot path doesn't need to special-case the no-DB
// runtime mode.
type Recorder struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewRecorder constructs a Recorder. Both pool and log may be nil;
// log defaults to slog.Default() in that case.
func NewRecorder(pool *pgxpool.Pool, log *slog.Logger) *Recorder {
	if log == nil {
		log = slog.Default()
	}
	return &Recorder{pool: pool, log: log}
}

// Record writes one Event to suite_cost_events. Returns nil on
// success or on a survivable failure (pool nil, write error). The
// caller (LLM gateway hook) treats every return as "carry on".
//
// Why best-effort: the cost ledger is observability data, not
// transactional state. A flaky PG should not cascade into 5xxs on
// /api/v1/llm/*. Failures are logged at Warn so operators still see
// them; counters in observability/ will surface persistent drops.
func (r *Recorder) Record(ctx context.Context, ev Event) error {
	if r == nil || r.pool == nil {
		// No DB wired: silently drop. The dashboard renders zeros.
		return nil
	}

	occurredAt := ev.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	// Compute total if the caller didn't set it. Providers that
	// report total separately can still override.
	total := ev.TotalTokens
	if total == 0 {
		total = ev.PromptTokens + ev.CompletionTokens
	}

	// Empty strings become NULL via *string. tenantPtr / apiKeyPtr go
	// to the FK columns; both have ON DELETE SET NULL so a deleted
	// tenant/key doesn't break aggregation of historical events.
	var tenantPtr, apiKeyPtr, agentPtr *string
	if ev.TenantID != "" {
		tid := ev.TenantID
		tenantPtr = &tid
	}
	if ev.APIKeyID != "" {
		aid := ev.APIKeyID
		apiKeyPtr = &aid
	}
	if ev.Agent != "" {
		ag := ev.Agent
		agentPtr = &ag
	}

	// Use a short-lived write context so a slow PG can't tie up the
	// LLM gateway's response goroutine.
	writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	modality := ev.Modality
	if modality == "" {
		modality = "text"
	}

	_, err := r.pool.Exec(writeCtx, `
        insert into suite_cost_events
            (tenant_id, api_key_id, model, provider, agent,
             prompt_tokens, completion_tokens, total_tokens,
             cost_usd, cached, latency_ms, occurred_at, modality)
        values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
    `,
		tenantPtr,
		apiKeyPtr,
		ev.Model,
		ev.Provider,
		agentPtr,
		ev.PromptTokens,
		ev.CompletionTokens,
		total,
		ev.CostUSD,
		ev.Cached,
		ev.LatencyMS,
		occurredAt,
		modality,
	)
	if err != nil {
		r.log.Warn("cost: ledger write failed",
			"model", ev.Model,
			"tenant", ev.TenantID,
			"error", err,
		)
		// Wrap so callers can choose to surface metrics on this path,
		// but never propagate as an LLM-call failure.
		return errors.Join(errors.New("cost: insert failed"), err)
	}
	return nil
}