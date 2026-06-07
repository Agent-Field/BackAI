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

// OpenAICompatProvider talks to an OpenAI-compatible HTTPS endpoint
// (OpenRouter, OpenAI direct, Together, anything that speaks the OpenAI
// REST shape).
//
// IMPORTANT CONTRACT NOTE — this is the Phase-7 MVP path. Public
// contract still says "every LLM call routes through AgentField". This
// provider exists purely because the AgentField instance running in
// `docker compose` does not yet expose a built-in `__llm.chat`
// reasoner. When AF gains that reasoner, swap this for AFProvider and
// the public API contract holds without a single caller-visible
// change.
//
// Provider id is reported as the `Name` field on construction so the
// cost-event payload (Phase 7.2) carries the right provider label
// (`openrouter`, `openai`, `anthropic`).
type OpenAICompatProvider struct {
	cfg    OpenAICompatConfig
	client *http.Client
}

// OpenAICompatConfig configures one OpenAI-compatible upstream.
type OpenAICompatConfig struct {
	// ProviderID is the value reported by Name() — used as the
	// `provider` field in CostEvent records (Phase 7.2).
	ProviderID string
	// BaseURL is the upstream root (e.g. https://openrouter.ai/api/v1).
	// Trailing slash is stripped.
	BaseURL string
	// APIKey is the bearer token sent to the upstream.
	APIKey string
	// ExtraHeaders are added to every outbound request (used e.g. for
	// OpenRouter's HTTP-Referer + X-Title hints).
	ExtraHeaders map[string]string
	// Timeout for non-streaming calls. Default 60s.
	Timeout time.Duration
}

// NewOpenAICompatProvider constructs a provider that proxies to the
// given OpenAI-compatible endpoint.
func NewOpenAICompatProvider(cfg OpenAICompatConfig) *OpenAICompatProvider {
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.ProviderID == "" {
		cfg.ProviderID = "openai-compat"
	}
	return &OpenAICompatProvider{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Name returns the provider id (used in cost-event labelling).
func (p *OpenAICompatProvider) Name() string {
	return p.cfg.ProviderID
}

// Chat performs a non-streaming chat completion.
func (p *OpenAICompatProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
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
	return out, nil
}

// ChatStream performs a streaming chat completion. Chunks are emitted
// as the upstream's SSE stream produces them.
func (p *OpenAICompatProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatStreamChunk, <-chan error) {
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
		// prefixes. OpenAI terminates with `data: [DONE]`.
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

// Embeddings creates one or more embedding vectors.
func (p *OpenAICompatProvider) Embeddings(ctx context.Context, req EmbeddingsRequest) (EmbeddingsResponse, error) {
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

// Images generates one or more images.
func (p *OpenAICompatProvider) Images(ctx context.Context, req ImagesRequest) (ImagesResponse, error) {
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

func (p *OpenAICompatProvider) newRequest(ctx context.Context, method, path string, body []byte, stream bool) (*http.Request, error) {
	if p.cfg.BaseURL == "" {
		return nil, &APIError{
			Code: ErrCodeUpstreamError, Message: "provider base URL not configured",
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
	if p.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}
	for k, v := range p.cfg.ExtraHeaders {
		req.Header.Set(k, v)
	}
	return req, nil
}

func (p *OpenAICompatProvider) do(ctx context.Context, method, path string, body []byte, stream bool) ([]byte, int, error) {
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
func (p *OpenAICompatProvider) upstreamErr(status int, body []byte) error {
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