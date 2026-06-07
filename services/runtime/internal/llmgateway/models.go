package llmgateway

import (
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/pricing"
)

// LLMModel mirrors LLMModelSchema in apps/dashboard/src/lib/api.ts.
//
// Field tags MUST stay snake_case so the dashboard's safeParse passes.
type LLMModel struct {
	ID                 string  `json:"id"`
	DisplayName        string  `json:"display_name"`
	Provider           string  `json:"provider"`
	PromptUSDPer1M     float64 `json:"prompt_usd_per_1m"`
	CompletionUSDPer1M float64 `json:"completion_usd_per_1m"`
	SupportsStreaming  bool    `json:"supports_streaming"`
	SupportsTools      bool    `json:"supports_tools"`
}

// LLMModelList mirrors LLMModelListSchema.
type LLMModelList struct {
	Models []LLMModel `json:"models"`
}

// ListModels returns every model in the pricing catalog, decorated
// with capability flags (streaming + tools).
//
// Capability table is intentionally static: the operator-facing
// dashboard needs to render these without a provider round-trip. When
// a model is added to pricing.Catalog without an entry here, the safe
// default is `supports_streaming=true, supports_tools=false` — every
// OpenAI-compat endpoint speaks SSE, and tool calling is a strict
// superset that we only advertise when we know the model supports it.
func ListModels() LLMModelList {
	out := LLMModelList{Models: make([]LLMModel, 0, len(pricing.Catalog))}
	for _, p := range pricing.Catalog {
		caps := capsFor(p.ID)
		out.Models = append(out.Models, LLMModel{
			ID:                 p.ID,
			DisplayName:        p.DisplayName,
			Provider:           p.Provider,
			PromptUSDPer1M:     p.PromptUSDPer1M,
			CompletionUSDPer1M: p.CompletionUSDPer1M,
			SupportsStreaming:  caps.Streaming,
			SupportsTools:      caps.Tools,
		})
	}
	return out
}

// modelCaps is the small static capability table.
type modelCaps struct {
	Streaming bool
	Tools     bool
}

// capsFor returns the capability flags for a given model id. Lookup is
// case-insensitive. Tools is conservative: only models the AF Stack
// team has verified end-to-end are listed as tool-capable here.
func capsFor(id string) modelCaps {
	switch strings.ToLower(strings.TrimSpace(id)) {
	// ─── OpenAI ─────────────────────────────────────────────────────
	case "openai/gpt-4o", "openai/gpt-4o-mini":
		return modelCaps{Streaming: true, Tools: true}

	// ─── Anthropic (direct) ────────────────────────────────────────
	case "anthropic/claude-haiku-4-5-20251001",
		"anthropic/claude-sonnet-4-6",
		"anthropic/claude-opus-4-7":
		return modelCaps{Streaming: true, Tools: true}

	// ─── OpenRouter — top-tier models support tools ────────────────
	case "openrouter/anthropic/claude-3.5-sonnet",
		"openrouter/qwen/qwen-2.5-72b-instruct",
		"openrouter/meta-llama/llama-3.3-70b-instruct":
		return modelCaps{Streaming: true, Tools: true}

	// ─── Smaller / open models — streaming yes, tools varies ──────
	case "openrouter/qwen/qwen-2.5-7b-instruct":
		return modelCaps{Streaming: true, Tools: false}
	case "openrouter/google/gemini-flash-1.5",
		"google/gemini-2.0-flash-exp":
		return modelCaps{Streaming: true, Tools: true}
	}
	// Safe default: streaming on, tools off.
	return modelCaps{Streaming: true, Tools: false}
}
