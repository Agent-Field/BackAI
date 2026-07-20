// SPDX-License-Identifier: Apache-2.0

// hooks.go — adapter glue that wires Recorder + Budgets to the
// hooks.Engine.
//
// The LLM gateway (Phase 7.1) doesn't import this package directly —
// it fires HookLLMPreCall / HookLLMPostCall with a flat payload map,
// and our handlers (registered in main.go) translate that into the
// Recorder/Budgets API.
//
// Payload contract (shared with the gateway):
//
//   pre_call:  map[string]any{
//     "tenant_id":     string (may be ""),
//     "model":         string,
//     "estimated_usd": float64 (optional, defaults to 0.001),
//   }
//
//   post_call: map[string]any{
//     "tenant_id":        string,
//     "api_key_id":       string,
//     "model":            string,
//     "provider":         string,
//     "agent":            string (optional),
//     "reasoner":         string (optional),
//     "prompt_tokens":    int,
//     "completion_tokens": int,
//     "total_tokens":     int (optional, derived if absent),
//     "cost_usd":         float64,
//     "cached":           bool,
//     "latency_ms":       int,
//     "status_code":      int,
//     "error_code":       string (optional),
//   }
//
// Why map[string]any instead of a typed struct: hooks.Engine carries
// payloads as `any`, and the gateway lives in a separate package that
// must not import this one (to avoid a cycle). The map keeps the
// contract loose — each side reads the fields it cares about and
// ignores extras.

package cost

import (
	"context"
	encjson "encoding/json"
	"fmt"

	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
)

var (
	jsonMarshal   = encjson.Marshal
	jsonUnmarshal = encjson.Unmarshal
)

// PreCallHandler returns a hooks.Handler that gates LLM calls on
// per-tenant budget. Wire with engine.Register(hooks.HookLLMPreCall,
// cost.PreCallHandler(budgets)) in main.go.
//
// On budget exhaustion the handler returns ErrBudgetExceeded so the
// gateway can short-circuit with HTTP 402. On DB errors the handler
// returns nil (fail-open) so a flaky PG can't take down /api/v1/llm/*.
func PreCallHandler(b *Budgets) hooks.Handler {
	return func(ctx context.Context, payload any) (any, error) {
		// The gateway fires a typed LLMPreCallPayload struct, not a map.
		// The old bare type assertion here always failed on it and the
		// "unknown shape -> admit" fallback silently waved every call
		// through — budgets were recorded but never enforced. Coerce the
		// same way PostCallHandler does.
		m := coerceToMap(payload)
		if m == nil {
			// Unknown shape -> admit. We never reject on parse failure.
			return payload, nil
		}
		tenantID, _ := m["tenant_id"].(string)
		if tenantID == "" {
			return payload, nil
		}
		estimated := 0.001
		// The gateway's json tag is estimated_cost_usd; accept the older
		// estimated_usd key too for map-shaped callers.
		if v, ok := m["estimated_cost_usd"].(float64); ok {
			estimated = v
		} else if v, ok := m["estimated_usd"].(float64); ok {
			estimated = v
		}
		ok2, err := b.HasBudget(ctx, tenantID, estimated)
		if err != nil {
			// DB error — fail-open. Surface via metrics, not via 5xx.
			return payload, nil
		}
		if !ok2 {
			return payload, fmt.Errorf("%w: tenant %s", ErrBudgetExceeded, tenantID)
		}
		// Per-key lifetime cap (suite_api_keys.budget_max_usd), enforced
		// runtime-side from the cost_events ledger so it works even when
		// LiteLLM's virtual-key budgets aren't wired. Same fail-open
		// contract: a DB error admits the call.
		if apiKeyID, _ := m["api_key_id"].(string); apiKeyID != "" {
			okKey, kerr := b.HasKeyBudget(ctx, apiKeyID, estimated)
			if kerr == nil && !okKey {
				return payload, fmt.Errorf("%w: api key %s", ErrKeyBudgetExceeded, apiKeyID)
			}
		}
		return payload, nil
	}
}

// PostCallHandler returns a hooks.Handler that records the LLM call
// to suite_cost_events. Always returns nil error — record failures
// are best-effort.
func PostCallHandler(r *Recorder) hooks.Handler {
	return func(ctx context.Context, payload any) (any, error) {
		m := coerceToMap(payload)
		if m == nil {
			return payload, nil
		}
		ev := Event{
			RequestID: stringFromMap(m, "request_id"),
			TenantID:  stringFromMap(m, "tenant_id"),
			APIKeyID:  stringFromMap(m, "api_key_id"),
			Model:     stringFromMap(m, "model"),
			Provider:  stringFromMap(m, "provider"),
			Agent:     stringFromMap(m, "agent"),
			Reasoner:  stringFromMap(m, "reasoner"),
			Modality:  stringFromMap(m, "modality"),

			PromptTokens:     intFromMap(m, "prompt_tokens"),
			CompletionTokens: intFromMap(m, "completion_tokens"),
			TotalTokens:      intFromMap(m, "total_tokens"),
			LatencyMS:        intFromMap(m, "latency_ms"),
			StatusCode:       intFromMap(m, "status_code"),
			ErrorCode:        stringFromMap(m, "error_code"),

			CostUSD: floatFromMap(m, "cost_usd"),
			Cached:  boolFromMap(m, "cached"),
		}
		// Drop the error to enforce best-effort semantics — Recorder
		// already logs the failure at Warn.
		_ = r.Record(ctx, ev)
		return payload, nil
	}
}

// coerceToMap normalises a hook payload to map[string]any so handlers
// work regardless of whether the caller fired a struct, a pointer to a
// struct, or a literal map. Uses encoding/json round-trip to honour the
// `json` tags (snake_case keys produced by the LLM gateway).
func coerceToMap(payload any) map[string]any {
	switch v := payload.(type) {
	case nil:
		return nil
	case map[string]any:
		return v
	}
	// Reflect through JSON: this gives us snake_case field names from
	// the source's json:"..." tags, which is exactly what stringFromMap
	// et al. look up.
	b, err := jsonMarshal(payload)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := jsonUnmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func stringFromMap(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// intFromMap tolerates both float64 (JSON's default number type) and
// int (Go callers constructing payloads directly).
func intFromMap(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func floatFromMap(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}

func boolFromMap(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}
