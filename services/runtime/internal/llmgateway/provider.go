// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"context"
	"encoding/json"
)

// Provider is the chat-gateway slot's abstract interface. The in-tree
// LiteLLMProvider satisfies it; remote-adapter shims under
// providers/remote/ satisfy it by talking the HTTP protocol from
// docs/adapters/protocols/llm-chat-v1.md.
//
// Multimodal verbs (TTS/STT/image gen) live on a separate slot
// (llmgateway/adapters.MultimodalAdapter). This interface is chat +
// embeddings + model discovery only.
//
// All methods take a context and respect cancellation.
type Provider interface {
	// Name returns the active adapter's identifier ("litellm",
	// "remote", "helicone", "portkey", ...). Surfaced to the
	// dashboard and stamped on cost events.
	Name() string

	// Chat performs a non-streaming chat completion.
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)

	// ChatStream performs a streaming chat completion. The chunk
	// channel emits each delta as it arrives. The error channel
	// emits exactly one value: nil on success, the terminal error
	// otherwise. Both channels close when the stream ends.
	ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatStreamChunk, <-chan error)

	// Embeddings returns vector embeddings for the input text(s).
	Embeddings(ctx context.Context, req EmbeddingsRequest) (EmbeddingsResponse, error)
}

// ProviderCapabilities is the typed view of the llm-chat capability
// envelope. Mirrors docs/adapters/protocols/llm-chat-v1.md §4.
type ProviderCapabilities struct {
	SupportsChat                  bool     `json:"supports_chat"`
	SupportsEmbeddings            bool     `json:"supports_embeddings"`
	SupportsStreaming             bool     `json:"supports_streaming"`
	SupportsTools                 bool     `json:"supports_tools"`
	SupportsVision                bool     `json:"supports_vision"`
	SupportsJSONMode              bool     `json:"supports_json_mode"`
	SupportsLogprobs              bool     `json:"supports_logprobs"`
	SupportsFallbackChains        bool     `json:"supports_fallback_chains"`
	SupportsPerTenantAttribution  bool     `json:"supports_per_tenant_attribution"`
	ModelPrefixes                 []string `json:"model_prefixes"`
	MaxCompletionTokens           int      `json:"max_completion_tokens"`
	MaxContextWindow              int      `json:"max_context_window"`
	RateLimitPerMinute            int      `json:"rate_limit_per_minute"`
	DefaultModel                  string   `json:"default_model"`
	FallbackChainDefault          []string `json:"fallback_chain_default"`
	TokeniserAvailable            bool     `json:"tokeniser_available"`
}

// ProviderCatalogModel mirrors one entry in GET /v1/models. The
// adapter MAY return extension fields the runtime ignores.
type ProviderCatalogModel struct {
	ID                          string   `json:"id"`
	Object                      string   `json:"object"`
	OwnedBy                     string   `json:"owned_by"`
	Verbs                       []string `json:"verbs"`
	ContextWindow               int      `json:"context_window,omitempty"`
	MaxOutputTokens             int      `json:"max_output_tokens,omitempty"`
	SupportsStreaming           bool     `json:"supports_streaming,omitempty"`
	SupportsTools               bool     `json:"supports_tools,omitempty"`
	SupportsVision              bool     `json:"supports_vision,omitempty"`
	SupportsJSONMode            bool     `json:"supports_json_mode,omitempty"`
	InputCostPerMillionTokens   float64  `json:"input_cost_per_million_tokens,omitempty"`
	OutputCostPerMillionTokens  float64  `json:"output_cost_per_million_tokens,omitempty"`
	EmbeddingDimensions         int      `json:"embedding_dimensions,omitempty"`
	DeprecationDate             string   `json:"deprecation_date,omitempty"`
}

// ProviderCatalog is the response of GET /v1/models.
type ProviderCatalog struct {
	Object string                 `json:"object"`
	Data   []ProviderCatalogModel `json:"data"`
}

// ProviderListMore is a small extension interface providers can
// implement if they expose more than the bare Provider contract — used
// by the dashboard for model-catalog views. Keeping this separate
// from Provider lets us add discovery surface without breaking the
// existing LiteLLMProvider.
type ProviderListMore interface {
	ListModels(ctx context.Context, verb string) (ProviderCatalog, error)
	Capabilities(ctx context.Context) (ProviderCapabilities, error)
}

// Compile-time assertion that LiteLLMProvider satisfies Provider.
// The richer ProviderListMore interface is satisfied by the remote
// shim; LiteLLM-specific catalog comes from a separate code path
// already (llmgateway/litellm_admin.go).
var _ Provider = (*LiteLLMProvider)(nil)

// rawCapabilities is the on-wire shape used for unmarshalling — the
// outer envelope mirrors remote.CapabilityResponse, but we re-declare
// here to keep llmgateway free of a dependency on the shared remote
// package (the per-slot remote shim does the bridging).
type rawCapabilities struct {
	Name            string          `json:"name"`
	Version         string          `json:"version"`
	Slot            string          `json:"slot"`
	ProtocolVersion string          `json:"protocol_version"`
	Capabilities    json.RawMessage `json:"capabilities"`
}
