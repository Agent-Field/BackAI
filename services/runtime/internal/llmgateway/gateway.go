package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/pricing"
)

// Gateway is the public façade for OpenAI-compatible LLM calls.
//
// Architecture: Gateway is provider-agnostic — it delegates the actual
// upstream HTTP call to a ProviderClient. Two implementations exist:
//
//   - AFProvider:      Routes the call into AgentField's built-in
//                      LLM-gateway reasoner. This is the canonical
//                      path and the one the public contract names.
//                      Wired by passing an *agentfield.Client and an
//                      AFReasonerCall (e.g. "__llm.chat").
//
//   - OpenAIProvider:  Calls an OpenAI-compatible HTTPS endpoint
//                      directly (OpenRouter, OpenAI, etc.). Used as
//                      the Phase-7 MVP path because the AgentField
//                      instance shipped in `docker compose` does not
//                      yet expose a `__llm.chat` reasoner. This is an
//                      INTERNAL implementation detail — the public
//                      contract still says "routes through AF". Swap
//                      to AFProvider once AF gains the built-in
//                      handler.
//
// Cost + hook orchestration (estimate, fire pre/post-call hooks,
// resolve tenant) belongs in the HTTP handler layer (server/llm.go),
// not here. Gateway is the shim; the handler is the policy.
type Gateway struct {
	provider ProviderClient
}

// ProviderClient is the abstraction Gateway delegates to. Tests can
// supply a fake; production wires an AF-routed or direct-OpenAI impl.
type ProviderClient interface {
	// Name identifies the provider in logs + the cost-event payload
	// emitted by HookLLMPostCall. Examples: "agentfield", "openrouter",
	// "openai", "anthropic", "fake".
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
}

// New constructs a Gateway with the given provider client.
//
// Pass NewOpenAICompatProvider(...) for the Phase-7 MVP path, or
// NewAFProvider(...) once AgentField exposes the built-in handler.
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
	ErrCodeModelRateLimited  = "MODEL_RATE_LIMITED"
	ErrCodeBudgetExceeded    = "BUDGET_EXCEEDED"
	ErrCodeUpstreamError     = "UPSTREAM_ERROR"
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

// Compile-time guard: ensure agentfield.Client is in our import set
// even when AFProvider hasn't been instantiated yet (so the import is
// not removed by goimports on a fresh checkout).
var _ = (*agentfield.Client)(nil)

// Now is the time source for the gateway. Overridable in tests so
// `Created` is deterministic.
var Now = func() int64 { return time.Now().Unix() }
