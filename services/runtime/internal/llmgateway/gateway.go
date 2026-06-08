// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/pricing"
)

// Gateway is the public façade for OpenAI-compatible LLM calls.
//
// Architecture: Gateway is provider-agnostic — it delegates the actual
// upstream HTTP call to a ProviderClient. The shipping implementation
// is LiteLLMProvider, which talks to a LiteLLM Proxy sidecar
// (`ghcr.io/berriai/litellm:main-stable`). LiteLLM handles the 100+
// upstream providers (OpenRouter, OpenAI, Anthropic, Google, Mistral,
// DeepSeek, Groq, Cohere, Bedrock, ...) so the runtime no longer ships
// a hand-rolled client per provider.
//
// The customer-visible surface is unchanged: callers point the OpenAI
// SDK at `/api/v1/llm`, and AF Stack keeps tenant resolution, cost
// ledger, budgets, cache, and hooks. LiteLLM is purely an internal
// implementation detail — customers never see it directly.
//
// Cost + hook orchestration (estimate, fire pre/post-call hooks,
// resolve tenant) belongs in the HTTP handler layer (server/llm.go),
// not here. Gateway is the shim; the handler is the policy.
type Gateway struct {
	provider   ProviderClient
	multimodal *Multimodal // optional; nil falls back to ProviderClient verbs
}

// ProviderClient is the abstraction Gateway delegates to. Tests can
// supply a fake; production wires a LiteLLM-backed impl.
type ProviderClient interface {
	// Name identifies the provider in logs + the cost-event payload
	// emitted by HookLLMPostCall. Production value is "litellm";
	// tests typically use "fake".
	Name() string
	// Chat performs a non-streaming chat completion.
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	// ChatStream performs a streaming chat completion. The provider
	// MUST close ch when the upstream stream terminates, and SHOULD
	// emit a final chunk with Usage populated when the upstream
	// supplies it. errCh receives at most one error.
	ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatStreamChunk, <-chan error)
	// Embeddings creates one or more embedding vectors.
	Embeddings(ctx context.Context, req EmbeddingsRequest) (EmbeddingsResponse, error)
	// Images generates one or more images.
	Images(ctx context.Context, req ImagesRequest) (ImagesResponse, error)
	// AudioSpeech generates speech audio from text.
	AudioSpeech(ctx context.Context, req AudioSpeechRequest) (RawResponse, error)
	// AudioTranscription transcribes audio to text.
	AudioTranscription(ctx context.Context, req AudioTranscriptionRequest) (RawResponse, error)
}

// New constructs a Gateway with the given provider client.
//
// Production wires NewLiteLLMProvider(...) pointed at the LiteLLM
// sidecar URL; tests pass a fakeProvider.
func New(provider ProviderClient) *Gateway {
	return &Gateway{provider: provider}
}

// ProviderName returns the provider id for log + cost-event labelling.
func (g *Gateway) ProviderName() string {
	if g == nil || g.provider == nil {
		return "unset"
	}
	return g.provider.Name()
}

// Chat performs a synchronous chat completion. Translates OpenAI-format
// in → upstream call → OpenAI-format out.
func (g *Gateway) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if g == nil || g.provider == nil {
		return ChatResponse{}, ErrNoProvider
	}
	if err := validateChat(req); err != nil {
		return ChatResponse{}, err
	}
	return g.provider.Chat(ctx, req)
}

// ChatStream performs a streaming chat completion. The returned chunk
// channel is closed when the upstream stream ends (success path); the
// error channel receives a non-nil error and is then closed on failure.
//
// Either channel may produce values; callers MUST consume both to
// avoid leaks (typical pattern: select on both, treat err channel as
// terminal).
func (g *Gateway) ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatStreamChunk, <-chan error) {
	if g == nil || g.provider == nil {
		errCh := make(chan error, 1)
		errCh <- ErrNoProvider
		close(errCh)
		chunkCh := make(chan ChatStreamChunk)
		close(chunkCh)
		return chunkCh, errCh
	}
	if err := validateChat(req); err != nil {
		errCh := make(chan error, 1)
		errCh <- err
		close(errCh)
		chunkCh := make(chan ChatStreamChunk)
		close(chunkCh)
		return chunkCh, errCh
	}
	return g.provider.ChatStream(ctx, req)
}

// Embeddings creates embeddings for one or more inputs.
func (g *Gateway) Embeddings(ctx context.Context, req EmbeddingsRequest) (EmbeddingsResponse, error) {
	if g == nil || g.provider == nil {
		return EmbeddingsResponse{}, ErrNoProvider
	}
	if strings.TrimSpace(req.Model) == "" {
		return EmbeddingsResponse{}, &APIError{Code: ErrCodeInvalidRequest, Message: "model is required"}
	}
	if req.Input == nil {
		return EmbeddingsResponse{}, &APIError{Code: ErrCodeInvalidRequest, Message: "input is required"}
	}
	return g.provider.Embeddings(ctx, req)
}

// Images generates one or more images.
func (g *Gateway) Images(ctx context.Context, req ImagesRequest) (ImagesResponse, error) {
	if g == nil || g.provider == nil {
		return ImagesResponse{}, ErrNoProvider
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return ImagesResponse{}, &APIError{Code: ErrCodeInvalidRequest, Message: "prompt is required"}
	}
	return g.provider.Images(ctx, req)
}

// AudioSpeech generates speech audio from text.
func (g *Gateway) AudioSpeech(ctx context.Context, req AudioSpeechRequest) (RawResponse, error) {
	if g == nil || g.provider == nil {
		return RawResponse{}, ErrNoProvider
	}
	if strings.TrimSpace(req.Model) == "" {
		return RawResponse{}, &APIError{Code: ErrCodeInvalidRequest, Message: "model is required"}
	}
	if strings.TrimSpace(req.Input) == "" {
		return RawResponse{}, &APIError{Code: ErrCodeInvalidRequest, Message: "input is required"}
	}
	if len(req.Body) == 0 {
		return RawResponse{}, &APIError{Code: ErrCodeInvalidRequest, Message: "request body is required"}
	}
	return g.provider.AudioSpeech(ctx, req)
}

// AudioTranscription transcribes audio to text.
func (g *Gateway) AudioTranscription(ctx context.Context, req AudioTranscriptionRequest) (RawResponse, error) {
	if g == nil || g.provider == nil {
		return RawResponse{}, ErrNoProvider
	}
	if strings.TrimSpace(req.Model) == "" {
		return RawResponse{}, &APIError{Code: ErrCodeInvalidRequest, Message: "model is required"}
	}
	if strings.TrimSpace(req.ContentType) == "" {
		return RawResponse{}, &APIError{Code: ErrCodeInvalidRequest, Message: "content-type is required"}
	}
	if len(req.Body) == 0 {
		return RawResponse{}, &APIError{Code: ErrCodeInvalidRequest, Message: "request body is required"}
	}
	return g.provider.AudioTranscription(ctx, req)
}

// EstimateCostUSD looks up the model in the pricing catalog and returns
// the cost for the given token usage. Convenience wrapper over the
// pricing package so handlers + tests can compute cost without
// importing pricing themselves.
func (g *Gateway) EstimateCostUSD(model string, promptTokens, completionTokens int) (float64, bool) {
	return pricing.EstimateCostUSD(model, promptTokens, completionTokens)
}

// validateChat enforces the minimum input contract.
func validateChat(req ChatRequest) error {
	if strings.TrimSpace(req.Model) == "" {
		return &APIError{Code: ErrCodeInvalidRequest, Message: "model is required"}
	}
	if len(req.Messages) == 0 {
		return &APIError{Code: ErrCodeInvalidRequest, Message: "messages must contain at least one entry"}
	}
	return nil
}

// ─── Error model ──────────────────────────────────────────────────────────

// Error codes returned in the OpenAI-style `error.code` field.
const (
	ErrCodeInvalidRequest    = "INVALID_REQUEST"
	ErrCodeModelNotSupported = "MODEL_NOT_SUPPORTED"
	// ErrCodeModelRateLimited is the 429 envelope code. Renamed from
	// the legacy MODEL_RATE_LIMITED to RATE_LIMIT_EXCEEDED with item
	// #32 — the runtime no longer runs a local token-bucket, so every
	// 429 originates from LiteLLM's per-virtual-key rpm_limit /
	// tpm_limit enforcement (item #22) and is surfaced with the
	// upstream Retry-After + X-RateLimit-* headers proxied through
	// verbatim. The Go symbol keeps the old name to preserve internal
	// call-site stability; only the wire value changed.
	ErrCodeModelRateLimited = "RATE_LIMIT_EXCEEDED"
	ErrCodeBudgetExceeded   = "BUDGET_EXCEEDED"
	ErrCodeUpstreamError    = "UPSTREAM_ERROR"
)

// ErrNoProvider is returned when Gateway has no provider wired. Indicates
// a misconfigured runtime (main.go forgot to construct one).
var ErrNoProvider = &APIError{
	Code:    "GATEWAY_NOT_CONFIGURED",
	Message: "llm gateway has no provider configured",
}

// APIError carries an OpenAI-style error code + message + status hint.
//
// HTTP handlers translate this into the OpenAI error envelope (see
// server/llm.go), which is the shape openai-python parses natively.
type APIError struct {
	// Code is the AF Stack error code (UPPER_SNAKE).
	Code string
	// Message is a human-readable summary.
	Message string
	// Status is the HTTP status to use when rendering. 0 means "the
	// handler decides", which lets ProviderClient implementations
	// stay agnostic to HTTP.
	Status int
	// Details is the optional structured detail payload (e.g. an
	// upstream provider's raw error body for debugging).
	Details map[string]any
	// Headers are response headers the handler should proxy back to
	// the client verbatim. Populated by ProviderClient implementations
	// for 429 responses so Retry-After / X-RateLimit-* survive the
	// upstream → runtime → SDK hop (item #32 — LiteLLM enforces RPM
	// upstream; we just surface its 429 + headers).
	Headers map[string]string
}

// Error satisfies the error interface.
func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Message
}

// HTTPStatus returns the rendering hint, defaulting to 502 (the most
// common case: upstream gave us trouble).
func (e *APIError) HTTPStatus() int {
	if e == nil {
		return http.StatusInternalServerError
	}
	if e.Status != 0 {
		return e.Status
	}
	switch e.Code {
	case ErrCodeInvalidRequest:
		return http.StatusBadRequest
	case ErrCodeBudgetExceeded:
		return http.StatusPaymentRequired
	case ErrCodeModelNotSupported:
		return http.StatusBadRequest
	case ErrCodeModelRateLimited:
		return http.StatusTooManyRequests
	case "GATEWAY_NOT_CONFIGURED":
		return http.StatusServiceUnavailable
	}
	return http.StatusBadGateway
}

// AsAPIError unwraps an APIError from err, returning (nil, false) if
// not present. Lets handlers branch on the structured fields without a
// type switch at every call site.
func AsAPIError(err error) (*APIError, bool) {
	if err == nil {
		return nil, false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// readBodyLimited reads up to maxBytes from r. Used by provider impls
// to bound memory when an upstream sends a runaway response.
func readBodyLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	if maxBytes <= 0 {
		maxBytes = 16 << 20 // 16MB default
	}
	return io.ReadAll(io.LimitReader(r, maxBytes))
}

// Now is the time source for the gateway. Overridable in tests so
// `Created` is deterministic.
var Now = func() int64 { return time.Now().Unix() }
