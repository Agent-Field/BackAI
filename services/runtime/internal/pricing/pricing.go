// Package pricing holds the canonical model-pricing table used by the cost
// endpoint and any budget/forecast feature.
//
// Numbers are USD per 1 million tokens, separated into prompt and
// completion. Sources: each provider's official pricing page, snapshot
// as of the date below. Update by editing this file — it's the single
// source of truth for the dashboard's Cost tab.
//
// Last updated: 2026-06-06.
//
// When the runtime can't find a model in the table it falls back to
// `Unknown`. The dashboard treats unknown-priced calls as `null` cost
// rather than `0`, so the operator knows the number is approximate.
package pricing

import "strings"

// ModelPrice describes pricing for one model in USD per 1M tokens.
type ModelPrice struct {
	// Canonical model id as routed by the LLM gateway (e.g.
	// "openrouter/qwen/qwen-2.5-72b-instruct").
	ID string
	// Human-readable display name for the dashboard.
	DisplayName string
	// Provider (anthropic, openai, openrouter, google).
	Provider string
	// USD per 1M prompt tokens.
	PromptUSDPer1M float64
	// USD per 1M completion tokens.
	CompletionUSDPer1M float64
}

// Unknown is returned when a model is not in the table. Cost is treated
// as nullable downstream so the operator isn't shown a fake $0.
var Unknown = ModelPrice{
	ID:          "unknown",
	DisplayName: "Unknown model",
	Provider:    "unknown",
}

// Catalog holds the supported models with cheap-but-good defaults near
// the top. Add new entries to the slice; lookup is case-insensitive on
// the id.
var Catalog = []ModelPrice{
	// ──────── OpenRouter — cheapest first ────────
	{
		ID:                 "openrouter/qwen/qwen-2.5-72b-instruct",
		DisplayName:        "Qwen 2.5 72B (OpenRouter)",
		Provider:           "openrouter",
		PromptUSDPer1M:     0.35,
		CompletionUSDPer1M: 0.40,
	},
	{
		ID:                 "openrouter/qwen/qwen-2.5-7b-instruct",
		DisplayName:        "Qwen 2.5 7B (OpenRouter)",
		Provider:           "openrouter",
		PromptUSDPer1M:     0.04,
		CompletionUSDPer1M: 0.10,
	},
	{
		ID:                 "openrouter/meta-llama/llama-3.3-70b-instruct",
		DisplayName:        "Llama 3.3 70B (OpenRouter)",
		Provider:           "openrouter",
		PromptUSDPer1M:     0.13,
		CompletionUSDPer1M: 0.40,
	},
	{
		ID:                 "openrouter/google/gemini-flash-1.5",
		DisplayName:        "Gemini Flash 1.5 (OpenRouter)",
		Provider:           "openrouter",
		PromptUSDPer1M:     0.075,
		CompletionUSDPer1M: 0.30,
	},
	{
		ID:                 "openrouter/anthropic/claude-3.5-sonnet",
		DisplayName:        "Claude 3.5 Sonnet (OpenRouter)",
		Provider:           "openrouter",
		PromptUSDPer1M:     3.00,
		CompletionUSDPer1M: 15.00,
	},

	// ──────── Anthropic direct ────────
	{
		ID:                 "anthropic/claude-haiku-4-5-20251001",
		DisplayName:        "Claude Haiku 4.5",
		Provider:           "anthropic",
		PromptUSDPer1M:     1.00,
		CompletionUSDPer1M: 5.00,
	},
	{
		ID:                 "anthropic/claude-sonnet-4-6",
		DisplayName:        "Claude Sonnet 4.6",
		Provider:           "anthropic",
		PromptUSDPer1M:     3.00,
		CompletionUSDPer1M: 15.00,
	},
	{
		ID:                 "anthropic/claude-opus-4-7",
		DisplayName:        "Claude Opus 4.7",
		Provider:           "anthropic",
		PromptUSDPer1M:     15.00,
		CompletionUSDPer1M: 75.00,
	},

	// ──────── OpenAI direct ────────
	{
		ID:                 "openai/gpt-4o-mini",
		DisplayName:        "GPT-4o mini",
		Provider:           "openai",
		PromptUSDPer1M:     0.15,
		CompletionUSDPer1M: 0.60,
	},
	{
		ID:                 "openai/gpt-4o",
		DisplayName:        "GPT-4o",
		Provider:           "openai",
		PromptUSDPer1M:     2.50,
		CompletionUSDPer1M: 10.00,
	},

	// ──────── Google direct ────────
	{
		ID:                 "google/gemini-2.0-flash-exp",
		DisplayName:        "Gemini 2.0 Flash",
		Provider:           "google",
		PromptUSDPer1M:     0.075,
		CompletionUSDPer1M: 0.30,
	},
}

// Lookup finds pricing for a given model id. Case-insensitive. Returns
// (price, true) on match, (Unknown, false) on miss.
func Lookup(modelID string) (ModelPrice, bool) {
	target := strings.ToLower(strings.TrimSpace(modelID))
	for _, p := range Catalog {
		if strings.ToLower(p.ID) == target {
			return p, true
		}
	}
	return Unknown, false
}

// EstimateCostUSD returns the dollar cost for a given token usage at the
// rates in the catalog. Returns (cost, true) on a hit, (0, false) on miss.
//
// Use this in the gateway request log to populate cost_usd when the
// provider exposes prompt+completion token counts.
func EstimateCostUSD(modelID string, promptTokens, completionTokens int) (float64, bool) {
	price, ok := Lookup(modelID)
	if !ok {
		return 0, false
	}
	cost := (float64(promptTokens)/1_000_000.0)*price.PromptUSDPer1M +
		(float64(completionTokens)/1_000_000.0)*price.CompletionUSDPer1M
	return cost, true
}
