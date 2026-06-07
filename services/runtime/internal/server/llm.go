// SPDX-License-Identifier: Apache-2.0

// llm.go — REST handlers for the OpenAI-compatible LLM gateway shim.
//
// Endpoints:
//
//	POST /api/v1/llm/chat/completions       — sync or streaming
//	POST /api/v1/llm/embeddings             — sync
//	POST /api/v1/llm/images/generations     — sync
//	GET  /api/v1/llm/models                 — pulls from pricing catalog
//	GET  /api/v1/llm/cache/stats            — Phase 7.3 cache module
//
// Wire shape: matches the OpenAI Python SDK so callers can do
//
//	client = openai.OpenAI(base_url=".../api/v1/llm", api_key="af_...")
//
// without modification. Errors are emitted in the OpenAI error
// envelope (`{error:{code,message,type}}`) instead of the AF Stack
// envelope, because that's the shape openai-python parses natively.
//
// Hook contract: every chat/embedding/image call fires HookLLMPreCall
// BEFORE the upstream and HookLLMPostCall AFTER. Phase 7.2's budget
// recorder hangs off these hooks; if HookLLMPreCall returns an error
// (e.g. BUDGET_EXCEEDED), the handler aborts without calling the
// upstream and the request is billed at $0.
//
// Tenant resolution relies on the Phase 6.1 tenant resolver having
// populated the context. Handlers MUST tolerate the empty-string case
// (no resolver wired) so unit tests + boot-mode flows still work.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// LLMPreCallPayload is the payload passed to HookLLMPreCall handlers.
//
// Fields are documented in TECH-SPEC.md §7.2 — budget guard reads
// `tenant_id`, `model`, `estimated_cost_usd` and rejects with
// BUDGET_EXCEEDED if the monthly cap would be exceeded.
type LLMPreCallPayload struct {
	TenantID         string  `json:"tenant_id,omitempty"`
	APIKeyID         string  `json:"api_key_id,omitempty"`
	Model            string  `json:"model"`
	Provider         string  `json:"provider"`
	RequestID        string  `json:"request_id"`
	Operation        string  `json:"operation"` // "chat" | "embeddings" | "images"
	Stream           bool    `json:"stream"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	PromptCharsHint  int     `json:"prompt_chars_hint"`
}

// LLMPostCallPayload is the payload passed to HookLLMPostCall handlers.
//
// Phase 7.2's Recorder writes this into suite_cost_events. Field names
// match the CostEvent zod schema (snake_case) so the dashboard renders
// without translation.
type LLMPostCallPayload struct {
	TenantID         string  `json:"tenant_id,omitempty"`
	APIKeyID         string  `json:"api_key_id,omitempty"`
	Model            string  `json:"model"`
	Provider         string  `json:"provider"`
	RequestID        string  `json:"request_id"`
	Operation        string  `json:"operation"`
	Stream           bool    `json:"stream"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	CostKnown        bool    `json:"cost_known"`
	Cached           bool    `json:"cached"`
	LatencyMS        int     `json:"latency_ms"`
	StatusCode       int     `json:"status_code"`
	ErrorCode        string  `json:"error_code,omitempty"`
	OccurredAt       string  `json:"occurred_at"`
}

// registerLLMRoutes wires the gateway endpoints. Called from
// registerRoutes() in server.go.
func (s *Server) registerLLMRoutes() {
	s.mux.HandleFunc("POST /api/v1/llm/chat/completions", s.handleLLMChatCompletions)
	s.mux.HandleFunc("POST /api/v1/llm/embeddings", s.handleLLMEmbeddings)
	s.mux.HandleFunc("POST /api/v1/llm/images/generations", s.handleLLMImages)
	s.mux.HandleFunc("GET /api/v1/llm/models", s.handleLLMModels)
	s.mux.HandleFunc("GET /api/v1/llm/cache/stats", s.handleLLMCacheStats)
}

// registerLLMOpenAPI describes /api/v1/llm/* in the OpenAPI 3.1 spec.
//
// All four POST routes accept the OpenAI-compat body shape; we keep
// the request schemas free-form here so the spec doesn't have to be
// kept in lockstep with every OpenAI field addition. Response
// schemas remain free-form too — clients use the OpenAI SDK typing.
func (s *Server) registerLLMOpenAPI() {
	b := s.openapi
	b.Register("POST", "/api/v1/llm/chat/completions", openapi.RouteMeta{
		Summary: "OpenAI-compatible chat completion (sync or SSE stream)",
		Tags:    []string{"llm"},
	})
	b.Register("POST", "/api/v1/llm/embeddings", openapi.RouteMeta{
		Summary: "OpenAI-compatible embeddings",
		Tags:    []string{"llm"},
	})
	b.Register("POST", "/api/v1/llm/images/generations", openapi.RouteMeta{
		Summary: "OpenAI-compatible image generation",
		Tags:    []string{"llm"},
	})
	b.Register("GET", "/api/v1/llm/models", openapi.RouteMeta{
		Summary: "List supported models with pricing + capability flags",
		Tags:    []string{"llm"},
	})
	b.Register("GET", "/api/v1/llm/cache/stats", openapi.RouteMeta{
		Summary: "LLM response-cache statistics",
		Tags:    []string{"llm"},
	})
}

// llmRequestID returns the X-Request-ID header if present, otherwise a
// monotonic per-process id. Used for log correlation + the
// request_id field of cost events.
func llmRequestID(r *http.Request) string {
	if v := r.Header.Get("X-Request-ID"); v != "" {
		return v
	}
	return "req-" + time.Now().UTC().Format("20060102T150405.000000000")
}

// writeOpenAIError emits an OpenAI-style error envelope, which the
// openai-python SDK parses natively. We use this for every /llm/*
// route so SDK consumers don't have to special-case our error shape.
func writeOpenAIError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{
		"error": map[string]any{
			"message": message,
			"code":    code,
			"type":    openAIErrorType(code),
		},
	}
	if details != nil {
		body["error"].(map[string]any)["details"] = details
	}
	_ = json.NewEncoder(w).Encode(body)
}

// openAIErrorType maps AF Stack codes to the OpenAI `error.type`
// taxonomy used by openai-python. The SDK branches on this field
// (e.g. RateLimitError vs APIError).
func openAIErrorType(code string) string {
	switch code {
	case llmgateway.ErrCodeInvalidRequest:
		return "invalid_request_error"
	case llmgateway.ErrCodeModelRateLimited:
		return "rate_limit_error"
	case llmgateway.ErrCodeBudgetExceeded:
		return "billing_error"
	case llmgateway.ErrCodeModelNotSupported:
		return "invalid_request_error"
	case llmgateway.ErrCodeUpstreamError:
		return "api_error"
	}
	return "api_error"
}

// llmAPIErrorResponse renders an llmgateway.APIError into an HTTP
// response with the right status code + envelope.
func llmAPIErrorResponse(w http.ResponseWriter, err error) {
	apiErr, ok := llmgateway.AsAPIError(err)
	if !ok {
		writeOpenAIError(w, http.StatusBadGateway,
			llmgateway.ErrCodeUpstreamError, err.Error(), nil)
		return
	}
	writeOpenAIError(w, apiErr.HTTPStatus(), apiErr.Code, apiErr.Message, apiErr.Details)
}

// estimatePromptChars returns a cheap character-count estimate of the
// prompt for the pre-call hook (so a budget guard can reject obvious
// overages without LLM tokenisation cost). Best-effort.
func estimatePromptChars(messages []llmgateway.ChatMessage) int {
	total := 0
	for _, m := range messages {
		switch v := m.Content.(type) {
		case string:
			total += len(v)
		case []any:
			// OpenAI multipart content (text + image_url). Count
			// only the textual parts.
			for _, part := range v {
				p, ok := part.(map[string]any)
				if !ok {
					continue
				}
				if t, ok := p["text"].(string); ok {
					total += len(t)
				}
			}
		}
	}
	return total
}

// ─── chat/completions ─────────────────────────────────────────────────────

func (s *Server) handleLLMChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := llmRequestID(r)
	if s.llmGateway == nil {
		writeOpenAIError(w, http.StatusServiceUnavailable,
			"GATEWAY_NOT_CONFIGURED",
			"llm gateway not configured on this runtime", nil)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "could not read request body", nil)
		return
	}
	var req llmgateway.ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "invalid JSON body: "+err.Error(), nil)
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "model is required", nil)
		return
	}
	if len(req.Messages) == 0 {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "messages must contain at least one entry", nil)
		return
	}

	tenantID := tenantctx.TenantID(r.Context())
	apiKeyID := tenantctx.APIKeyID(r.Context())
	promptChars := estimatePromptChars(req.Messages)
	// Conservative cost estimate: assume prompt-chars / 4 = tokens
	// (the standard OpenAI heuristic) and complete with 512 tokens.
	estPrompt := promptChars / 4
	estCompletion := 512
	estCost, _ := s.llmGateway.EstimateCostUSD(req.Model, estPrompt, estCompletion)

	pre := LLMPreCallPayload{
		TenantID:         tenantID,
		APIKeyID:         apiKeyID,
		Model:            req.Model,
		Provider:         s.llmGateway.ProviderName(),
		RequestID:        requestID,
		Operation:        "chat",
		Stream:           req.Stream,
		EstimatedCostUSD: estCost,
		PromptCharsHint:  promptChars,
	}
	if err := s.fireLLMPreCall(r.Context(), pre); err != nil {
		s.writeHookRejection(w, err, requestID, start, tenantID, apiKeyID, req.Model, "chat", req.Stream)
		return
	}

	if req.Stream {
		s.streamChatCompletion(w, r, req, requestID, start, pre)
		return
	}

	resp, err := s.llmGateway.Chat(r.Context(), req)
	post := s.buildPostPayload(pre, &resp, nil, start, http.StatusOK)

	if err != nil {
		s.fireLLMPostCallBest(r.Context(), s.errorPost(pre, err, start))
		llmAPIErrorResponse(w, err)
		return
	}
	s.fireLLMPostCallBest(r.Context(), post)
	writeJSON(w, http.StatusOK, resp)
}

// streamChatCompletion runs a streaming chat completion. The upstream
// emits ChatStreamChunk events; we forward them as `data: {...}\n\n`
// SSE events and finish with `data: [DONE]\n\n`.
func (s *Server) streamChatCompletion(
	w http.ResponseWriter,
	r *http.Request,
	req llmgateway.ChatRequest,
	requestID string,
	start time.Time,
	pre LLMPreCallPayload,
) {
	sse, err := llmgateway.NewSSEWriter(w)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError,
			llmgateway.ErrCodeUpstreamError, "streaming not supported by this connection", nil)
		return
	}

	chunkCh, errCh := s.llmGateway.ChatStream(r.Context(), req)

	// Aggregate token usage from the final chunk that carries it
	// (OpenAI emits usage on the last chunk when stream_options
	// {"include_usage": true} is set; we record what we get).
	var lastUsage *llmgateway.Usage
	streamErr := error(nil)

streamLoop:
	for {
		select {
		case chunk, ok := <-chunkCh:
			if !ok {
				break streamLoop
			}
			if chunk.Usage != nil {
				u := *chunk.Usage
				lastUsage = &u
			}
			if err := sse.WriteChunk(chunk); err != nil {
				streamErr = err
				break streamLoop
			}
		case err, ok := <-errCh:
			if !ok {
				// errCh closed without an error event — wait for chunks
				// to drain.
				continue
			}
			if err != nil {
				streamErr = err
				break streamLoop
			}
		case <-r.Context().Done():
			streamErr = r.Context().Err()
			break streamLoop
		}
	}

	if streamErr != nil {
		// Render the in-band error before [DONE] so openai-python
		// raises a structured exception.
		code := llmgateway.ErrCodeUpstreamError
		msg := streamErr.Error()
		if apiErr, ok := llmgateway.AsAPIError(streamErr); ok {
			code = apiErr.Code
			msg = apiErr.Message
		}
		sse.WriteError(code, msg)
		s.fireLLMPostCallBest(r.Context(), s.errorPost(pre, streamErr, start))
		return
	}

	_ = sse.End()
	post := s.buildPostPayload(pre, nil, lastUsage, start, http.StatusOK)
	s.fireLLMPostCallBest(r.Context(), post)
}

// ─── embeddings ───────────────────────────────────────────────────────────

func (s *Server) handleLLMEmbeddings(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := llmRequestID(r)
	if s.llmGateway == nil {
		writeOpenAIError(w, http.StatusServiceUnavailable,
			"GATEWAY_NOT_CONFIGURED",
			"llm gateway not configured on this runtime", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "could not read request body", nil)
		return
	}
	var req llmgateway.EmbeddingsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "invalid JSON body: "+err.Error(), nil)
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "model is required", nil)
		return
	}
	if req.Input == nil {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "input is required", nil)
		return
	}

	tenantID := tenantctx.TenantID(r.Context())
	apiKeyID := tenantctx.APIKeyID(r.Context())
	pre := LLMPreCallPayload{
		TenantID:  tenantID,
		APIKeyID:  apiKeyID,
		Model:     req.Model,
		Provider:  s.llmGateway.ProviderName(),
		RequestID: requestID,
		Operation: "embeddings",
	}
	if err := s.fireLLMPreCall(r.Context(), pre); err != nil {
		s.writeHookRejection(w, err, requestID, start, tenantID, apiKeyID, req.Model, "embeddings", false)
		return
	}

	resp, err := s.llmGateway.Embeddings(r.Context(), req)
	if err != nil {
		s.fireLLMPostCallBest(r.Context(), s.errorPost(pre, err, start))
		llmAPIErrorResponse(w, err)
		return
	}
	post := s.buildPostPayloadEmbeddings(pre, resp, start)
	s.fireLLMPostCallBest(r.Context(), post)
	writeJSON(w, http.StatusOK, resp)
}

// ─── images/generations ──────────────────────────────────────────────────

func (s *Server) handleLLMImages(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := llmRequestID(r)
	if s.llmGateway == nil {
		writeOpenAIError(w, http.StatusServiceUnavailable,
			"GATEWAY_NOT_CONFIGURED",
			"llm gateway not configured on this runtime", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "could not read request body", nil)
		return
	}
	var req llmgateway.ImagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "invalid JSON body: "+err.Error(), nil)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "prompt is required", nil)
		return
	}

	tenantID := tenantctx.TenantID(r.Context())
	apiKeyID := tenantctx.APIKeyID(r.Context())
	pre := LLMPreCallPayload{
		TenantID:  tenantID,
		APIKeyID:  apiKeyID,
		Model:     req.Model,
		Provider:  s.llmGateway.ProviderName(),
		RequestID: requestID,
		Operation: "images",
	}
	if err := s.fireLLMPreCall(r.Context(), pre); err != nil {
		s.writeHookRejection(w, err, requestID, start, tenantID, apiKeyID, req.Model, "images", false)
		return
	}

	resp, err := s.llmGateway.Images(r.Context(), req)
	if err != nil {
		s.fireLLMPostCallBest(r.Context(), s.errorPost(pre, err, start))
		llmAPIErrorResponse(w, err)
		return
	}
	post := LLMPostCallPayload{
		TenantID:   pre.TenantID,
		APIKeyID:   pre.APIKeyID,
		Model:      pre.Model,
		Provider:   pre.Provider,
		RequestID:  pre.RequestID,
		Operation:  pre.Operation,
		Stream:     false,
		LatencyMS:  int(time.Since(start).Milliseconds()),
		StatusCode: http.StatusOK,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	s.fireLLMPostCallBest(r.Context(), post)
	writeJSON(w, http.StatusOK, resp)
}

// ─── /llm/models ─────────────────────────────────────────────────────────

func (s *Server) handleLLMModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, llmgateway.ListModels())
}

// ─── /llm/cache/stats ─────────────────────────────────────────────────────

// llmCacheStats matches CacheStatsSchema in apps/dashboard/src/lib/api.ts.
//
// Phase 7.3 owns the cache module; until it lands, this returns zeros
// so the dashboard renders without errors.
type llmCacheStats struct {
	TotalCalls  int     `json:"total_calls"`
	CacheHits   int     `json:"cache_hits"`
	CacheMisses int     `json:"cache_misses"`
	HitRate     float64 `json:"hit_rate"`
	SavingsUSD  float64 `json:"savings_usd"`
	Entries     int     `json:"entries"`
}

func (s *Server) handleLLMCacheStats(w http.ResponseWriter, r *http.Request) {
	// Phase 7.3 owns llmcache. When wired we proxy to its Stats(); when
	// not wired we emit a structurally-valid response with zero values
	// so the dashboard renders.
	if s.llmCache == nil {
		writeJSON(w, http.StatusOK, llmCacheStats{})
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	st, err := s.llmCache.Stats(r.Context(), tenantID)
	if err != nil {
		s.log.Warn("llm cache stats failed", "tenant", tenantID, "error", err)
		writeJSON(w, http.StatusOK, llmCacheStats{})
		return
	}
	writeJSON(w, http.StatusOK, llmCacheStats{
		TotalCalls:  int(st.TotalCalls),
		CacheHits:   int(st.CacheHits),
		CacheMisses: int(st.CacheMisses),
		HitRate:     st.HitRate,
		SavingsUSD:  st.SavingsUSD,
		Entries:     int(st.Entries),
	})
}

// ─── Hook plumbing ───────────────────────────────────────────────────────

// fireLLMPreCall fires HookLLMPreCall and returns its error (if any).
// Non-fatal when no hook engine is wired (e.g. unit tests).
func (s *Server) fireLLMPreCall(ctx context.Context, payload LLMPreCallPayload) error {
	if s.hooks == nil {
		return nil
	}
	if _, err := s.hooks.Fire(ctx, hooks.HookLLMPreCall, payload); err != nil {
		return err
	}
	return nil
}

// fireLLMPostCallBest fires HookLLMPostCall and logs any error.
// "Best" = best-effort; a failing post-call hook must not affect the
// response that's already been written.
func (s *Server) fireLLMPostCallBest(ctx context.Context, payload LLMPostCallPayload) {
	if s.hooks == nil {
		return
	}
	if _, err := s.hooks.Fire(ctx, hooks.HookLLMPostCall, payload); err != nil {
		s.log.Warn("llm post-call hook error",
			"request_id", payload.RequestID,
			"model", payload.Model,
			"error", err)
	}
}

// writeHookRejection maps a pre-call hook error (typically
// BUDGET_EXCEEDED from Phase 7.2's recorder) into the right status
// code + envelope. Also fires HookLLMPostCall so the rejection is
// recorded — Phase 7.2's audit log wants to see the attempt even
// when the budget guard blocks the upstream.
func (s *Server) writeHookRejection(
	w http.ResponseWriter,
	err error,
	requestID string,
	start time.Time,
	tenantID, apiKeyID, model, operation string,
	stream bool,
) {
	code, msg, status := classifyHookError(err)
	writeOpenAIError(w, status, code, msg, nil)
	post := LLMPostCallPayload{
		TenantID:   tenantID,
		APIKeyID:   apiKeyID,
		Model:      model,
		Provider:   s.llmGateway.ProviderName(),
		RequestID:  requestID,
		Operation:  operation,
		Stream:     stream,
		LatencyMS:  int(time.Since(start).Milliseconds()),
		StatusCode: status,
		ErrorCode:  code,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	s.fireLLMPostCallBest(context.Background(), post)
}

// classifyHookError inspects a pre-call hook error and returns the
// (code, message, status) tuple to render. Phase 7.2's budget guard
// returns an APIError with code BUDGET_EXCEEDED; anything else is
// rendered as a generic FORBIDDEN.
func classifyHookError(err error) (string, string, int) {
	var apiErr *llmgateway.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code, apiErr.Message, apiErr.HTTPStatus()
	}
	return "POLICY_VIOLATION", err.Error(), http.StatusForbidden
}

// buildPostPayload constructs the post-call hook payload for a chat
// completion. When resp is non-nil it's used to extract usage; if
// usage is supplied (streaming path) it overrides resp.Usage.
func (s *Server) buildPostPayload(
	pre LLMPreCallPayload,
	resp *llmgateway.ChatResponse,
	usage *llmgateway.Usage,
	start time.Time,
	status int,
) LLMPostCallPayload {
	post := LLMPostCallPayload{
		TenantID:   pre.TenantID,
		APIKeyID:   pre.APIKeyID,
		Model:      pre.Model,
		Provider:   pre.Provider,
		RequestID:  pre.RequestID,
		Operation:  pre.Operation,
		Stream:     pre.Stream,
		LatencyMS:  int(time.Since(start).Milliseconds()),
		StatusCode: status,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	var u *llmgateway.Usage
	switch {
	case usage != nil:
		u = usage
	case resp != nil && resp.Usage != nil:
		u = resp.Usage
	}
	if u != nil {
		post.PromptTokens = u.PromptTokens
		post.CompletionTokens = u.CompletionTokens
		post.TotalTokens = u.TotalTokens
		if cost, ok := s.llmGateway.EstimateCostUSD(pre.Model, u.PromptTokens, u.CompletionTokens); ok {
			post.CostUSD = cost
			post.CostKnown = true
		}
	}
	return post
}

// buildPostPayloadEmbeddings constructs the post-call payload for an
// embeddings response (tokens may be provided in the Usage field).
func (s *Server) buildPostPayloadEmbeddings(
	pre LLMPreCallPayload,
	resp llmgateway.EmbeddingsResponse,
	start time.Time,
) LLMPostCallPayload {
	post := LLMPostCallPayload{
		TenantID:   pre.TenantID,
		APIKeyID:   pre.APIKeyID,
		Model:      pre.Model,
		Provider:   pre.Provider,
		RequestID:  pre.RequestID,
		Operation:  pre.Operation,
		LatencyMS:  int(time.Since(start).Milliseconds()),
		StatusCode: http.StatusOK,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if resp.Usage != nil {
		post.PromptTokens = resp.Usage.PromptTokens
		post.CompletionTokens = resp.Usage.CompletionTokens
		post.TotalTokens = resp.Usage.TotalTokens
		if cost, ok := s.llmGateway.EstimateCostUSD(pre.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens); ok {
			post.CostUSD = cost
			post.CostKnown = true
		}
	}
	return post
}

// errorPost constructs a post-call payload for a failed call (so
// Phase 7.2's recorder still sees the attempt).
func (s *Server) errorPost(pre LLMPreCallPayload, err error, start time.Time) LLMPostCallPayload {
	code := llmgateway.ErrCodeUpstreamError
	status := http.StatusBadGateway
	if apiErr, ok := llmgateway.AsAPIError(err); ok {
		code = apiErr.Code
		status = apiErr.HTTPStatus()
	}
	return LLMPostCallPayload{
		TenantID:   pre.TenantID,
		APIKeyID:   pre.APIKeyID,
		Model:      pre.Model,
		Provider:   pre.Provider,
		RequestID:  pre.RequestID,
		Operation:  pre.Operation,
		Stream:     pre.Stream,
		LatencyMS:  int(time.Since(start).Milliseconds()),
		StatusCode: status,
		ErrorCode:  code,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}