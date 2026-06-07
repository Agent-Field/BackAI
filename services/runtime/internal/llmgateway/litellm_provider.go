// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LiteLLMProvider talks to a LiteLLM Proxy sidecar (or any OpenAI-compatible
// HTTPS endpoint that follows the same shape).
//
// Architecture: AF Stack runs LiteLLM as a sidecar container
// (image `ghcr.io/berriai/litellm:main-stable`). LiteLLM handles the
// 100+ upstream providers (OpenRouter, OpenAI, Anthropic, Google,
// Mistral, DeepSeek, Groq, Cohere, Bedrock, ...) — our gateway no
// longer hand-rolls a client per provider.
//
// The customer-visible surface (`/api/v1/llm/*`) is unchanged. AF Stack
// keeps tenant resolution, cost ledger, budgets, cache, hooks, and the
// OpenAI-compatible shape on its side; LiteLLM is purely upstream
// multi-provider routing.
//
// Cost: when LiteLLM is configured with model pricing it injects
// `response_cost` and `_response_ms` into the response JSON. The gateway
// surfaces this via Usage extension when present, and falls back to
// the static pricing.EstimateCostUSD table otherwise.
type LiteLLMProvider struct {
	cfg    LiteLLMConfig
	client *http.Client
}

// LiteLLMConfig configures the LiteLLM sidecar URL + auth.
type LiteLLMConfig struct {
	// ProviderID is the value reported by Name() — used as the
	// `provider` field in CostEvent records. Defaults to "litellm".
	ProviderID string
	// BaseURL is the LiteLLM proxy root (e.g. http://litellm:4000).
	// Trailing slash is stripped.
	BaseURL string
	// MasterKey is the LITELLM_MASTER_KEY shared with the sidecar.
	// Internal sidecar auth — NOT exposed to customers.
	MasterKey string
	// ExtraHeaders are added to every outbound request. Reserved for
	// future tagging (e.g. tenant id surfacing into LiteLLM's
	// per-customer logging when needed).
	ExtraHeaders map[string]string
	// Timeout for non-streaming calls. Default 60s.
	Timeout time.Duration
}

// NewLiteLLMProvider constructs a provider that proxies to the given
// LiteLLM sidecar.
func NewLiteLLMProvider(cfg LiteLLMConfig) *LiteLLMProvider {
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.ProviderID == "" {
		cfg.ProviderID = "litellm"
	}
	return &LiteLLMProvider{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Name returns the provider id (used in cost-event labelling).
func (p *LiteLLMProvider) Name() string {
	return p.cfg.ProviderID
}

// Chat performs a non-streaming chat completion via LiteLLM.
func (p *LiteLLMProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	// Force stream=false; the streaming path has its own method.
	body := chatRequestBody(req, false)

	respBody, status, err := p.do(ctx, "POST", "/chat/completions", body, false)
	if err != nil {
		return ChatResponse{}, err
	}
	if status >= 400 {
		return ChatResponse{}, p.upstreamErr(status, respBody)
	}

	var out ChatResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return ChatResponse{}, &APIError{
			Code:    ErrCodeUpstreamError,
			Message: fmt.Sprintf("decode upstream response: %v", err),
			Status:  http.StatusBadGateway,
			Details: map[string]any{"upstream_body_preview": previewBody(respBody)},
		}
	}

	// LiteLLM injects `response_cost` (USD) and `_response_ms` at the
	// top level when its config has pricing for the requested model.
	// Surface them via Usage so the gateway hook layer can prefer
	// LiteLLM's number over the static pricing.EstimateCostUSD fallback.
	if cost, ok := extractResponseCost(respBody); ok {
		if out.Usage == nil {
			out.Usage = &Usage{}
		}
		out.Usage.ResponseCostUSD = &cost
	}
	return out, nil
}

// ChatStream performs a streaming chat completion. Chunks are emitted
// as LiteLLM's SSE stream produces them.
func (p *LiteLLMProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatStreamChunk, <-chan error) {
	chunkCh := make(chan ChatStreamChunk, 16)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		body := chatRequestBody(req, true)
		httpReq, err := p.newRequest(ctx, "POST", "/chat/completions", body, true)
		if err != nil {
			errCh <- err
			return
		}

		// Streaming responses use a tail-style client with no overall
		// read deadline — only the underlying TCP idle timeout. The
		// gateway client has Timeout set, which closes connections
		// even mid-stream; build a one-off client for the stream path.
		streamClient := &http.Client{Timeout: 0}
		resp, err := streamClient.Do(httpReq)
		if err != nil {
			errCh <- &APIError{
				Code:    ErrCodeUpstreamError,
				Message: fmt.Sprintf("upstream stream connect: %v", err),
				Status:  http.StatusBadGateway,
			}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			b, _ := readBodyLimited(resp.Body, 1<<20)
			errCh <- p.upstreamErr(resp.StatusCode, b)
			return
		}

		// Walk the SSE stream line by line, splitting on `data: `
		// prefixes. LiteLLM (like OpenAI) terminates with `data: [DONE]`.
		scanner := bufio.NewScanner(resp.Body)
		// Allow large SSE lines (some chunks include verbose tool
		// arguments). 1MB headroom per line is the OpenAI ceiling.
		buf := make([]byte, 0, 1<<16)
		scanner.Buffer(buf, 1<<20)

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				// Comments + `event:` lines are not relevant to the
				// OpenAI shape; skip silently.
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				return
			}
			var chunk ChatStreamChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				// Tolerate malformed individual chunks rather than
				// killing the stream — log via the error channel only
				// if the whole stream fails afterwards.
				continue
			}
			// LiteLLM surfaces response_cost on the final chunk too
			// (when it has pricing). Forward it through Usage so the
			// gateway's post-call hook can prefer it.
			if chunk.Usage != nil {
				if cost, ok := extractResponseCost([]byte(payload)); ok {
					chunk.Usage.ResponseCostUSD = &cost
				}
			}
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case chunkCh <- chunk:
			}
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			errCh <- &APIError{
				Code:    ErrCodeUpstreamError,
				Message: fmt.Sprintf("upstream stream read: %v", err),
				Status:  http.StatusBadGateway,
			}
		}
	}()

	return chunkCh, errCh
}

// Embeddings creates one or more embedding vectors via LiteLLM.
func (p *LiteLLMProvider) Embeddings(ctx context.Context, req EmbeddingsRequest) (EmbeddingsResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return EmbeddingsResponse{}, &APIError{
			Code: ErrCodeInvalidRequest, Message: "encode embeddings request: " + err.Error(),
		}
	}
	respBody, status, err := p.do(ctx, "POST", "/embeddings", body, false)
	if err != nil {
		return EmbeddingsResponse{}, err
	}
	if status >= 400 {
		return EmbeddingsResponse{}, p.upstreamErr(status, respBody)
	}
	var out EmbeddingsResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return EmbeddingsResponse{}, &APIError{
			Code: ErrCodeUpstreamError, Message: "decode embeddings response: " + err.Error(),
		}
	}
	return out, nil
}

// Images generates one or more images via LiteLLM.
func (p *LiteLLMProvider) Images(ctx context.Context, req ImagesRequest) (ImagesResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return ImagesResponse{}, &APIError{
			Code: ErrCodeInvalidRequest, Message: "encode images request: " + err.Error(),
		}
	}
	respBody, status, err := p.do(ctx, "POST", "/images/generations", body, false)
	if err != nil {
		return ImagesResponse{}, err
	}
	if status >= 400 {
		return ImagesResponse{}, p.upstreamErr(status, respBody)
	}
	var out ImagesResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return ImagesResponse{}, &APIError{
			Code: ErrCodeUpstreamError, Message: "decode images response: " + err.Error(),
		}
	}
	return out, nil
}

// ─── HTTP plumbing ───────────────────────────────────────────────────────

func (p *LiteLLMProvider) newRequest(ctx context.Context, method, path string, body []byte, stream bool) (*http.Request, error) {
	if p.cfg.BaseURL == "" {
		return nil, &APIError{
			Code: ErrCodeUpstreamError, Message: "litellm base URL not configured",
			Status: http.StatusServiceUnavailable,
		}
	}
	url := p.cfg.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	// Internal sidecar auth. The LITELLM_MASTER_KEY is shared between
	// the runtime and the LiteLLM container at compose-time; customers
	// never see it. Their tenant key is checked one layer earlier.
	if p.cfg.MasterKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.MasterKey)
	}
	for k, v := range p.cfg.ExtraHeaders {
		req.Header.Set(k, v)
	}
	return req, nil
}

func (p *LiteLLMProvider) do(ctx context.Context, method, path string, body []byte, stream bool) ([]byte, int, error) {
	httpReq, err := p.newRequest(ctx, method, path, body, stream)
	if err != nil {
		return nil, 0, err
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, 0, &APIError{
			Code:    ErrCodeUpstreamError,
			Message: fmt.Sprintf("upstream call: %v", err),
			Status:  http.StatusBadGateway,
		}
	}
	defer resp.Body.Close()
	respBody, _ := readBodyLimited(resp.Body, 16<<20)
	return respBody, resp.StatusCode, nil
}

// upstreamErr converts a non-2xx upstream response into an APIError
// with the OpenAI-compat error code mapped from the status.
func (p *LiteLLMProvider) upstreamErr(status int, body []byte) error {
	code := ErrCodeUpstreamError
	switch status {
	case http.StatusBadRequest:
		code = ErrCodeInvalidRequest
	case http.StatusTooManyRequests:
		code = ErrCodeModelRateLimited
	case http.StatusNotFound:
		code = ErrCodeModelNotSupported
	}

	// Try to extract upstream's error.message for the human-facing
	// message; fall back to a generic one when the body isn't JSON.
	msg := fmt.Sprintf("upstream returned status %d", status)
	if extracted := extractUpstreamMessage(body); extracted != "" {
		msg = extracted
	}

	return &APIError{
		Code:    code,
		Message: msg,
		Status:  status,
		Details: map[string]any{
			"upstream_status":       status,
			"upstream_body_preview": previewBody(body),
		},
	}
}

// chatRequestBody marshals req with stream forced to the given value.
//
// We use a sibling shape rather than mutating the caller's req so the
// handler can stay agnostic about which path it dispatched on.
func chatRequestBody(req ChatRequest, stream bool) []byte {
	// Re-encode through a generic map so unknown OpenAI fields in
	// req.Extra are forwarded to the upstream (LiteLLM tolerates
	// provider-specific fields like `safety_settings`,
	// `repetition_penalty`).
	type outBody struct {
		ChatRequest
		Stream bool `json:"stream"`
	}
	out := outBody{ChatRequest: req, Stream: stream}
	// Force Stream from the wrapper, not from the embedded struct.
	out.ChatRequest.Stream = false
	b, _ := json.Marshal(out)
	return b
}

// extractUpstreamMessage pulls the "error.message" string out of a
// provider response body. Returns "" on any failure.
func extractUpstreamMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var aux struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &aux); err != nil {
		return ""
	}
	return aux.Error.Message
}

// extractResponseCost pulls LiteLLM's `response_cost` field (top-level
// USD float) out of a JSON body. LiteLLM injects this when the proxy
// config maps the model to a pricing entry — we surface it so the
// runtime's cost ledger prefers LiteLLM's number over the static
// pricing.EstimateCostUSD fallback.
//
// Returns (cost, true) on hit; (0, false) when the field is absent or
// the body isn't JSON.
func extractResponseCost(body []byte) (float64, bool) {
	if len(body) == 0 {
		return 0, false
	}
	var aux struct {
		ResponseCost *float64 `json:"response_cost"`
	}
	if err := json.Unmarshal(body, &aux); err != nil {
		return 0, false
	}
	if aux.ResponseCost == nil {
		return 0, false
	}
	return *aux.ResponseCost, true
}

// previewBody returns a UTF-8-safe trimmed preview of the upstream's
// body for logging / error.details. Truncates to 512 bytes so a giant
// blob doesn't show up in the dashboard.
func previewBody(body []byte) string {
	const max = 512
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "…"
}
