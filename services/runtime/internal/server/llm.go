// SPDX-License-Identifier: Apache-2.0

// llm.go — REST handlers for the OpenAI-compatible LLM gateway shim.
//
// Endpoints:
//
//	POST /api/v1/llm/chat/completions       — sync or streaming
//	POST /api/v1/llm/embeddings             — sync
//	POST /api/v1/embeddings                 — sync, canonical suite path
//	POST /api/v1/llm/images/generations     — sync
//	POST /api/v1/images/generations         — sync, canonical suite path
//	POST /api/v1/audio/speech               — sync, returns audio bytes
//	POST /api/v1/audio/transcriptions       — sync, multipart upload
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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/appmetrics"
	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/guardrails"
	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/llmcache"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// attachLiteLLMKey resolves the per-tenant LiteLLM virtual-key secret
// (stored by tenancy.IssueAPIKey under "litellm/key/{api_key_id}") and
// attaches it to the request context via llmgateway.WithLiteLLMKey.
//
// The LiteLLM provider reads the key off the context and uses it as
// the Authorization header instead of the master key, so LiteLLM
// attributes spend + enforces budget/rate-limit against the customer's
// virtual key.
//
// Returns the context unchanged when:
//   - no tenancy manager is wired,
//   - no secrets vault is wired,
//   - the api key has no LiteLLM mapping (legacy rows / boot without
//     LiteLLM admin configured).
//
// Empty key falls back to LITELLM_MASTER_KEY — preserving pre-#22
// behaviour for every degraded path.
func (s *Server) attachLiteLLMKey(ctx context.Context) context.Context {
	if s == nil || s.tenancy == nil {
		return ctx
	}
	tenantID := tenantctx.TenantID(ctx)
	apiKeyID := tenantctx.APIKeyID(ctx)
	if tenantID == "" || apiKeyID == "" {
		return ctx
	}
	key, err := s.tenancy.LiteLLMKeyForAPIKey(ctx, tenantID, apiKeyID)
	if err != nil {
		// Vault unreachable / transient error — log and fall back to
		// master key. The customer's call still succeeds; LiteLLM
		// just won't see the per-key budget enforcement for this
		// request.
		s.log.Warn("llm: resolve litellm key failed",
			"tenant_id", tenantID, "api_key_id", apiKeyID, "error", err)
		return ctx
	}
	if key == "" {
		return ctx
	}
	return llmgateway.WithLiteLLMKey(ctx, key)
}

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
	Operation        string  `json:"operation"` // "chat" | "embeddings" | "images" | "audio_speech" | "audio_transcriptions" | "audio_translations" | "images_edits" | "images_variations"
	Stream           bool    `json:"stream"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	PromptCharsHint  int     `json:"prompt_chars_hint"`
	// Modality classifies the call for the cost ledger. See the
	// Modality* constants for accepted values. Defaults to "text".
	Modality string `json:"modality,omitempty"`
}

// Modality constants — written verbatim into suite_cost_events.modality
// and surfaced in the dashboard's "cost by modality" breakdown.
const (
	ModalityText               = "text"
	ModalityEmbedding          = "embedding"
	ModalityAudioSpeech        = "audio_speech"
	ModalityAudioTranscription = "audio_transcription"
	ModalityAudioTranslation   = "audio_translation"
	ModalityImage              = "image"
	ModalityVideo              = "video"
)

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
	TTFTSeconds      float64 `json:"ttft_seconds,omitempty"`
	StatusCode       int     `json:"status_code"`
	ErrorCode        string  `json:"error_code,omitempty"`
	OccurredAt       string  `json:"occurred_at"`
	// Modality classifies the call for the cost ledger. Defaults to
	// "text". See ModalityText / ModalityImage / ... constants.
	Modality string `json:"modality,omitempty"`
}

// registerLLMRoutes wires the gateway endpoints. Called from
// registerRoutes() in server.go.
func (s *Server) registerLLMRoutes() {
	s.mux.HandleFunc("POST /api/v1/llm/chat/completions", s.handleLLMChatCompletions)
	s.mux.HandleFunc("POST /api/v1/llm/embeddings", s.handleLLMEmbeddings)
	s.mux.HandleFunc("POST /api/v1/embeddings", s.handleLLMEmbeddings)
	s.mux.HandleFunc("POST /api/v1/llm/images/generations", s.handleLLMImages)
	s.mux.HandleFunc("POST /api/v1/images/generations", s.handleLLMImages)
	s.mux.HandleFunc("POST /api/v1/llm/images/edits", s.handleLLMImageEdits)
	s.mux.HandleFunc("POST /api/v1/images/edits", s.handleLLMImageEdits)
	s.mux.HandleFunc("POST /api/v1/llm/images/variations", s.handleLLMImageVariations)
	s.mux.HandleFunc("POST /api/v1/images/variations", s.handleLLMImageVariations)
	s.mux.HandleFunc("POST /api/v1/audio/speech", s.handleLLMAudioSpeech)
	s.mux.HandleFunc("POST /api/v1/audio/transcriptions", s.handleLLMAudioTranscriptions)
	s.mux.HandleFunc("POST /api/v1/audio/translations", s.handleLLMAudioTranslations)
	s.mux.HandleFunc("GET /api/v1/llm/models", s.handleLLMModels)
	s.mux.HandleFunc("GET /api/v1/llm/cache/stats", s.handleLLMCacheStats)
	s.mux.HandleFunc("POST /api/v1/llm/cache/flush", s.handleLLMCacheFlush)
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
	b.Register("POST", "/api/v1/embeddings", openapi.RouteMeta{
		Summary: "OpenAI-compatible embeddings",
		Tags:    []string{"llm"},
	})
	b.Register("POST", "/api/v1/llm/images/generations", openapi.RouteMeta{
		Summary: "OpenAI-compatible image generation",
		Tags:    []string{"llm"},
	})
	b.Register("POST", "/api/v1/images/generations", openapi.RouteMeta{
		Summary: "OpenAI-compatible image generation",
		Tags:    []string{"llm"},
	})
	b.Register("POST", "/api/v1/audio/speech", openapi.RouteMeta{
		Summary: "OpenAI-compatible text to speech",
		Tags:    []string{"llm"},
	})
	b.Register("POST", "/api/v1/audio/transcriptions", openapi.RouteMeta{
		Summary: "OpenAI-compatible audio transcription",
		Tags:    []string{"llm"},
	})
	b.Register("POST", "/api/v1/audio/translations", openapi.RouteMeta{
		Summary: "OpenAI-compatible audio translation (to English)",
		Tags:    []string{"llm"},
	})
	b.Register("POST", "/api/v1/images/edits", openapi.RouteMeta{
		Summary: "OpenAI-compatible image edit (multipart upload)",
		Tags:    []string{"llm"},
	})
	b.Register("POST", "/api/v1/llm/images/edits", openapi.RouteMeta{
		Summary: "OpenAI-compatible image edit through the LLM namespace",
		Tags:    []string{"llm"},
	})
	b.Register("POST", "/api/v1/images/variations", openapi.RouteMeta{
		Summary: "OpenAI-compatible image variations (multipart upload)",
		Tags:    []string{"llm"},
	})
	b.Register("POST", "/api/v1/llm/images/variations", openapi.RouteMeta{
		Summary: "OpenAI-compatible image variations through the LLM namespace",
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
	b.Register("POST", "/api/v1/llm/cache/flush", openapi.RouteMeta{
		Summary: "Flush LLM response-cache entries",
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
	writeOpenAIErrorWithHeaders(w, status, code, message, details, nil)
}

// writeOpenAIErrorWithHeaders is the variant that sets extra response
// headers before writing the body. Used for 429 passthrough so
// Retry-After + X-RateLimit-* from LiteLLM reach the client (item #32 —
// rate limiting is enforced upstream by LiteLLM, the runtime just
// proxies the response).
//
// Any header set on w MUST be set BEFORE WriteHeader; this helper bundles
// the two steps so callers can't forget.
func writeOpenAIErrorWithHeaders(
	w http.ResponseWriter,
	status int,
	code, message string,
	details map[string]any,
	headers map[string]string,
) {
	for k, v := range headers {
		if v == "" {
			continue
		}
		w.Header().Set(k, v)
	}
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
	case guardrails.CodeContentBlocked:
		return "invalid_request_error"
	case llmgateway.ErrCodeUpstreamError:
		return "api_error"
	}
	return "api_error"
}

// llmAPIErrorResponse renders an llmgateway.APIError into an HTTP
// response with the right status code + envelope.
//
// When the APIError carries upstream rate-limit headers (Retry-After /
// X-RateLimit-*), they're proxied verbatim to the client — LiteLLM
// owns rate-limit enforcement (item #22 — per-virtual-key rpm_limit /
// tpm_limit), so the runtime just surfaces the upstream's response
// signal. Item #32 removed the local in-process limiter; this is the
// only path 429s flow through now.
func llmAPIErrorResponse(w http.ResponseWriter, err error) {
	apiErr, ok := llmgateway.AsAPIError(err)
	if !ok {
		writeOpenAIError(w, http.StatusBadGateway,
			llmgateway.ErrCodeUpstreamError, err.Error(), nil)
		return
	}
	writeOpenAIErrorWithHeaders(w, apiErr.HTTPStatus(), apiErr.Code, apiErr.Message, apiErr.Details, apiErr.Headers)
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

func estimatedTokensFromChars(chars int) int {
	if chars <= 0 {
		return 0
	}
	tokens := chars / 4
	if tokens == 0 {
		return 1
	}
	return tokens
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
	if s.guardrails != nil {
		next, changed, err := s.guardrails.ProcessChatRequest(r.Context(), req)
		if err != nil {
			s.writeGuardrailRejection(w, err, requestID, start, tenantID, apiKeyID, req.Model, "chat", req.Stream)
			return
		}
		if changed {
			req = next
		}
	}
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

	resp, err := s.llmGateway.Chat(s.attachLiteLLMKey(r.Context()), req)
	post := s.buildPostPayload(pre, &resp, nil, start, http.StatusOK)

	if err != nil {
		s.fireLLMPostCallBest(r.Context(), s.errorPost(pre, err, start))
		llmAPIErrorResponse(w, err)
		return
	}
	if s.guardrails != nil {
		next, _, err := s.guardrails.ProcessChatResponse(r.Context(), resp)
		if err != nil {
			s.fireLLMPostCallBest(r.Context(), s.guardrailPost(pre, err, start))
			writeGuardrailError(w, err)
			return
		}
		resp = next
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

	chunkCh, errCh := s.llmGateway.ChatStream(s.attachLiteLLMKey(r.Context()), req)

	// Aggregate token usage from the final chunk that carries it
	// (OpenAI emits usage on the last chunk when stream_options
	// {"include_usage": true} is set; we record what we get).
	var lastUsage *llmgateway.Usage
	completionChars := 0
	streamErr := error(nil)
	firstChunkAt := time.Time{}

streamLoop:
	for {
		select {
		case chunk, ok := <-chunkCh:
			if !ok {
				break streamLoop
			}
			if firstChunkAt.IsZero() {
				firstChunkAt = time.Now()
			}
			if chunk.Usage != nil {
				u := *chunk.Usage
				lastUsage = &u
			}
			for _, choice := range chunk.Choices {
				if text, ok := choice.Delta.Content.(string); ok {
					completionChars += len(text)
				}
			}
			if s.guardrails != nil {
				next, _, err := s.guardrails.ProcessChatStreamChunk(r.Context(), chunk)
				if err != nil {
					streamErr = err
					break streamLoop
				}
				chunk = next
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
		} else if gCode, gMsg, _ := classifyGuardrailError(streamErr); gCode != "" {
			code = gCode
			msg = gMsg
		}
		sse.WriteError(code, msg)
		if isGuardrailError(streamErr) {
			s.fireLLMPostCallBest(r.Context(), s.guardrailPost(pre, streamErr, start))
		} else {
			s.fireLLMPostCallBest(r.Context(), s.errorPost(pre, streamErr, start))
		}
		return
	}

	_ = sse.End()
	if lastUsage == nil || lastUsage.TotalTokens == 0 {
		promptTokens := estimatedTokensFromChars(pre.PromptCharsHint)
		completionTokens := estimatedTokensFromChars(completionChars)
		if promptTokens > 0 || completionTokens > 0 {
			lastUsage = &llmgateway.Usage{
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
				TotalTokens:      promptTokens + completionTokens,
			}
		}
	}
	post := s.buildPostPayload(pre, nil, lastUsage, start, http.StatusOK)
	if !firstChunkAt.IsZero() {
		post.TTFTSeconds = firstChunkAt.Sub(start).Seconds()
	}
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
	if s.guardrails != nil {
		next, changed, err := s.guardrails.ProcessEmbeddingsRequest(r.Context(), req)
		if err != nil {
			s.writeGuardrailRejection(w, err, requestID, start, tenantID, apiKeyID, req.Model, "embeddings", false)
			return
		}
		if changed {
			req = next
		}
	}
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

	resp, err := s.llmGateway.Embeddings(s.attachLiteLLMKey(r.Context()), req)
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
	if s.guardrails != nil {
		next, changed, err := s.guardrails.ProcessImagesRequest(r.Context(), req)
		if err != nil {
			s.writeGuardrailRejection(w, err, requestID, start, tenantID, apiKeyID, req.Model, "images", false)
			return
		}
		if changed {
			req = next
		}
	}
	pre := LLMPreCallPayload{
		TenantID:  tenantID,
		APIKeyID:  apiKeyID,
		Model:     req.Model,
		Provider:  s.llmGateway.ProviderName(),
		RequestID: requestID,
		Operation: "images",
		Modality:  ModalityImage,
	}
	if err := s.fireLLMPreCall(r.Context(), pre); err != nil {
		s.writeHookRejection(w, err, requestID, start, tenantID, apiKeyID, req.Model, "images", false)
		return
	}

	n := 0
	if req.N != nil {
		n = *req.N
	}
	resp, provider, err := s.llmGateway.Image(s.attachLiteLLMKey(r.Context()), adapters.ImageRequest{
		Model:          req.Model,
		Prompt:         req.Prompt,
		N:              n,
		Size:           req.Size,
		Quality:        req.Quality,
		Style:          req.Style,
		ResponseFormat: req.ResponseFormat,
		Body:           body,
	})
	if err != nil {
		s.fireLLMPostCallBest(r.Context(), s.errorPost(pre, err, start))
		llmAPIErrorResponse(w, err)
		return
	}
	if s.guardrails != nil {
		for i := range resp.Data {
			if resp.Data[i].RevisedPrompt == "" {
				continue
			}
			res, err := s.guardrails.ProcessText(r.Context(), resp.Data[i].RevisedPrompt)
			if err != nil {
				s.fireLLMPostCallBest(r.Context(), s.guardrailPost(pre, err, start))
				writeGuardrailError(w, err)
				return
			}
			resp.Data[i].RevisedPrompt = res.Text
		}
	}
	post := s.simplePostPayload(pre, start, http.StatusOK)
	if provider != "" {
		post.Provider = provider
	}
	// Image cost = per-image rate × N.
	if cnt := len(resp.Data); cnt > 0 {
		if cost, ok := llmgateway.EstimateMultimodalCost(req.Model, "image", float64(cnt)); ok {
			post.CostUSD = cost
			post.CostKnown = true
		}
	}
	s.fireLLMPostCallBest(r.Context(), post)
	writeJSON(w, http.StatusOK, llmgateway.ImagesResponseFromAdapter(resp))
}

// handleLLMImageEdits handles POST /api/v1/images/edits (multipart).
//
// Multipart fields: image (file), prompt (string), mask (optional file),
// model (string), n (string), size (string), response_format (string).
//
// We forward the multipart body verbatim to the chosen adapter; the
// adapter takes care of any re-encoding (LiteLLM tunnels it straight to
// OpenAI; first-party adapters that don't support edits return
// ErrUnsupported and the runtime renders 503).
func (s *Server) handleLLMImageEdits(w http.ResponseWriter, r *http.Request) {
	s.handleLLMImageMultipart(w, r, true, false)
}

// handleLLMImageVariations handles POST /api/v1/images/variations (multipart).
//
// Multipart fields: image (file), model (string), n (string),
// size (string), response_format (string).
func (s *Server) handleLLMImageVariations(w http.ResponseWriter, r *http.Request) {
	s.handleLLMImageMultipart(w, r, false, true)
}

// handleLLMImageMultipart is the shared multipart-image path for edits
// and variations.
func (s *Server) handleLLMImageMultipart(w http.ResponseWriter, r *http.Request, edit, variations bool) {
	start := time.Now()
	requestID := llmRequestID(r)
	operation := "images_edits"
	if variations {
		operation = "images_variations"
	}
	if s.llmGateway == nil {
		writeOpenAIError(w, http.StatusServiceUnavailable,
			"GATEWAY_NOT_CONFIGURED",
			"llm gateway not configured on this runtime", nil)
		return
	}
	ct := r.Header.Get("Content-Type")
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "could not read request body", nil)
		return
	}
	meta, err := inspectImageMultipart(body, ct, edit, variations)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, err.Error(), nil)
		return
	}

	tenantID := tenantctx.TenantID(r.Context())
	apiKeyID := tenantctx.APIKeyID(r.Context())
	pre := LLMPreCallPayload{
		TenantID:  tenantID,
		APIKeyID:  apiKeyID,
		Model:     meta.Model,
		Provider:  s.llmGateway.ProviderName(),
		RequestID: requestID,
		Operation: operation,
		Modality:  ModalityImage,
	}
	if err := s.fireLLMPreCall(r.Context(), pre); err != nil {
		s.writeHookRejection(w, err, requestID, start, tenantID, apiKeyID, meta.Model, operation, false)
		return
	}

	resp, provider, err := s.llmGateway.Image(s.attachLiteLLMKey(r.Context()), adapters.ImageRequest{
		Model:                meta.Model,
		Prompt:               meta.Prompt,
		N:                    meta.N,
		Size:                 meta.Size,
		ResponseFormat:       meta.ResponseFormat,
		IsEdit:               edit,
		IsVariations:         variations,
		MultipartContentType: ct,
		Body:                 body,
	})
	if err != nil {
		s.fireLLMPostCallBest(r.Context(), s.errorPost(pre, err, start))
		llmAPIErrorResponse(w, err)
		return
	}
	post := s.simplePostPayload(pre, start, http.StatusOK)
	if provider != "" {
		post.Provider = provider
	}
	if cnt := len(resp.Data); cnt > 0 {
		if cost, ok := llmgateway.EstimateMultimodalCost(meta.Model, "image", float64(cnt)); ok {
			post.CostUSD = cost
			post.CostKnown = true
		}
	}
	s.fireLLMPostCallBest(r.Context(), post)
	writeJSON(w, http.StatusOK, llmgateway.ImagesResponseFromAdapter(resp))
}

// ─── audio/speech ────────────────────────────────────────────────────────

func (s *Server) handleLLMAudioSpeech(w http.ResponseWriter, r *http.Request) {
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
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "invalid JSON body: "+err.Error(), nil)
		return
	}
	model, _ := raw["model"].(string)
	input, _ := raw["input"].(string)
	voice, _ := raw["voice"].(string)
	respFormat, _ := raw["response_format"].(string)
	var speed float64
	switch v := raw["speed"].(type) {
	case float64:
		speed = v
	case int:
		speed = float64(v)
	}
	if strings.TrimSpace(model) == "" {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "model is required", nil)
		return
	}
	if strings.TrimSpace(input) == "" {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "input is required", nil)
		return
	}

	tenantID := tenantctx.TenantID(r.Context())
	apiKeyID := tenantctx.APIKeyID(r.Context())
	if s.guardrails != nil {
		next, changed, err := s.guardrails.ProcessStringMap(r.Context(), raw, "input", "user")
		if err != nil {
			s.writeGuardrailRejection(w, err, requestID, start, tenantID, apiKeyID, model, "audio_speech", false)
			return
		}
		if changed {
			raw = next
			if v, ok := raw["input"].(string); ok {
				input = v
			}
			body, _ = json.Marshal(raw)
		}
	}
	pre := LLMPreCallPayload{
		TenantID:  tenantID,
		APIKeyID:  apiKeyID,
		Model:     model,
		Provider:  s.llmGateway.ProviderName(),
		RequestID: requestID,
		Operation: "audio_speech",
		Modality:  ModalityAudioSpeech,
	}
	if err := s.fireLLMPreCall(r.Context(), pre); err != nil {
		s.writeHookRejection(w, err, requestID, start, tenantID, apiKeyID, model, "audio_speech", false)
		return
	}

	resp, provider, err := s.llmGateway.Speech(s.attachLiteLLMKey(r.Context()), adapters.SpeechRequest{
		Model:          model,
		Input:          input,
		Voice:          voice,
		ResponseFormat: respFormat,
		Speed:          speed,
		Body:           body,
	})
	if err != nil {
		s.fireLLMPostCallBest(r.Context(), s.errorPost(pre, err, start))
		llmAPIErrorResponse(w, err)
		return
	}
	if s.guardrails != nil {
		next, _, err := s.guardrails.ProcessResponseBytes(r.Context(), resp.Body, resp.ContentType)
		if err != nil {
			s.fireLLMPostCallBest(r.Context(), s.guardrailPost(pre, err, start))
			writeGuardrailError(w, err)
			return
		}
		resp.Body = next
	}
	post := s.simplePostPayload(pre, start, http.StatusOK)
	if provider != "" {
		post.Provider = provider
	}
	// Per-character pricing for TTS — uses pricing.EstimateMultimodalCost.
	if resp.CharCount > 0 {
		if cost, ok := llmgateway.EstimateMultimodalCost(model, "tts", float64(resp.CharCount)); ok {
			post.CostUSD = cost
			post.CostKnown = true
		}
	}
	s.fireLLMPostCallBest(r.Context(), post)
	writeRawSpeech(w, http.StatusOK, resp)
}

// ─── audio/transcriptions ────────────────────────────────────────────────

func (s *Server) handleLLMAudioTranscriptions(w http.ResponseWriter, r *http.Request) {
	s.handleLLMTranscribe(w, r, false)
}

// ─── audio/translations ──────────────────────────────────────────────────

func (s *Server) handleLLMAudioTranslations(w http.ResponseWriter, r *http.Request) {
	s.handleLLMTranscribe(w, r, true)
}

// handleLLMTranscribe is the shared path for STT + translate. Both
// endpoints accept multipart/form-data and differ only in the upstream
// path (LiteLLM /audio/transcriptions vs /audio/translations).
func (s *Server) handleLLMTranscribe(w http.ResponseWriter, r *http.Request, translate bool) {
	start := time.Now()
	requestID := llmRequestID(r)
	operation := "audio_transcriptions"
	modality := ModalityAudioTranscription
	if translate {
		operation = "audio_translations"
		modality = ModalityAudioTranslation
	}
	if s.llmGateway == nil {
		writeOpenAIError(w, http.StatusServiceUnavailable,
			"GATEWAY_NOT_CONFIGURED",
			"llm gateway not configured on this runtime", nil)
		return
	}
	ct := r.Header.Get("Content-Type")
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, "could not read request body", nil)
		return
	}
	model, err := inspectTranscriptionMultipart(body, ct)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest,
			llmgateway.ErrCodeInvalidRequest, err.Error(), nil)
		return
	}

	tenantID := tenantctx.TenantID(r.Context())
	apiKeyID := tenantctx.APIKeyID(r.Context())
	pre := LLMPreCallPayload{
		TenantID:  tenantID,
		APIKeyID:  apiKeyID,
		Model:     model,
		Provider:  s.llmGateway.ProviderName(),
		RequestID: requestID,
		Operation: operation,
		Modality:  modality,
	}
	if err := s.fireLLMPreCall(r.Context(), pre); err != nil {
		s.writeHookRejection(w, err, requestID, start, tenantID, apiKeyID, model, operation, false)
		return
	}

	resp, provider, err := s.llmGateway.Transcribe(s.attachLiteLLMKey(r.Context()), adapters.TranscribeRequest{
		Model:       model,
		ContentType: ct,
		Body:        body,
		Translate:   translate,
	})
	if err != nil {
		s.fireLLMPostCallBest(r.Context(), s.errorPost(pre, err, start))
		llmAPIErrorResponse(w, err)
		return
	}
	post := s.simplePostPayload(pre, start, http.StatusOK)
	if provider != "" {
		post.Provider = provider
	}
	// STT cost = per-minute rate × audio_seconds / 60. Adapters that
	// surface DurationSeconds (currently nil — TODO) feed this number.
	if resp.DurationSeconds > 0 {
		if cost, ok := llmgateway.EstimateMultimodalCost(model, "stt", resp.DurationSeconds); ok {
			post.CostUSD = cost
			post.CostKnown = true
		}
	}
	s.fireLLMPostCallBest(r.Context(), post)
	writeRawTranscribe(w, http.StatusOK, resp)
}

func inspectTranscriptionMultipart(body []byte, contentType string) (string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/form-data") {
		return "", errors.New("content-type must be multipart/form-data")
	}
	boundary := params["boundary"]
	if boundary == "" {
		return "", errors.New("multipart boundary is required")
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	model := ""
	hasFile := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", errors.New("invalid multipart body")
		}
		switch part.FormName() {
		case "model":
			value, _ := io.ReadAll(io.LimitReader(part, 1024))
			model = strings.TrimSpace(string(value))
		case "file":
			if part.FileName() != "" {
				hasFile = true
			}
		}
		_ = part.Close()
	}
	if model == "" {
		return "", errors.New("model is required")
	}
	if !hasFile {
		return "", errors.New("file is required")
	}
	return model, nil
}

func writeRaw(w http.ResponseWriter, status int, resp llmgateway.RawResponse) {
	if resp.ContentType != "" {
		w.Header().Set("Content-Type", resp.ContentType)
	}
	w.WriteHeader(status)
	_, _ = w.Write(resp.Body)
}

// writeRawSpeech renders an adapters.SpeechResponse as the HTTP body.
// Default content-type is audio/mpeg when the adapter didn't supply one.
func writeRawSpeech(w http.ResponseWriter, status int, resp adapters.SpeechResponse) {
	ct := resp.ContentType
	if ct == "" {
		ct = "audio/mpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(status)
	_, _ = w.Write(resp.Body)
}

// writeRawTranscribe renders an adapters.TranscribeResponse.
func writeRawTranscribe(w http.ResponseWriter, status int, resp adapters.TranscribeResponse) {
	ct := resp.ContentType
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(status)
	_, _ = w.Write(resp.Body)
}

// imageMultipartMeta is the small struct we extract from an /images/edits
// or /images/variations multipart upload to drive routing + post-call
// accounting. The actual file bytes stay in the original body — adapters
// that tunnel to LiteLLM re-send it verbatim.
type imageMultipartMeta struct {
	Model          string
	Prompt         string
	N              int
	Size           string
	ResponseFormat string
}

// inspectImageMultipart walks the multipart body and pulls out the
// non-file fields needed for routing and cost accounting. Validates
// that an `image` file field is present (edits + variations both
// require it).
func inspectImageMultipart(body []byte, contentType string, edit, variations bool) (imageMultipartMeta, error) {
	out := imageMultipartMeta{}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/form-data") {
		return out, errors.New("content-type must be multipart/form-data")
	}
	boundary := params["boundary"]
	if boundary == "" {
		return out, errors.New("multipart boundary is required")
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	hasImage := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, errors.New("invalid multipart body")
		}
		switch part.FormName() {
		case "model":
			value, _ := io.ReadAll(io.LimitReader(part, 1024))
			out.Model = strings.TrimSpace(string(value))
		case "prompt":
			value, _ := io.ReadAll(io.LimitReader(part, 32<<10))
			out.Prompt = strings.TrimSpace(string(value))
		case "n":
			value, _ := io.ReadAll(io.LimitReader(part, 32))
			fmt.Sscanf(strings.TrimSpace(string(value)), "%d", &out.N)
		case "size":
			value, _ := io.ReadAll(io.LimitReader(part, 64))
			out.Size = strings.TrimSpace(string(value))
		case "response_format":
			value, _ := io.ReadAll(io.LimitReader(part, 32))
			out.ResponseFormat = strings.TrimSpace(string(value))
		case "image":
			if part.FileName() != "" {
				hasImage = true
			}
		}
		_ = part.Close()
	}
	if out.Model == "" {
		return out, errors.New("model is required")
	}
	if !hasImage {
		return out, errors.New("image file is required")
	}
	if edit && out.Prompt == "" {
		return out, errors.New("prompt is required for /images/edits")
	}
	_ = variations // signature symmetry only
	return out, nil
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

func (s *Server) handleLLMCacheFlush(w http.ResponseWriter, r *http.Request) {
	if s.llmCache == nil {
		writeError(w, http.StatusServiceUnavailable,
			"LLM_CACHE_NOT_CONFIGURED",
			"llm cache is not configured on this runtime", nil)
		return
	}
	q := r.URL.Query()
	opts := llmcache.FlushOpts{
		TenantID:   strings.TrimSpace(q.Get("tenant")),
		PromptHash: strings.TrimSpace(q.Get("prompt_hash")),
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
			return
		}
		if len(strings.TrimSpace(string(body))) > 0 {
			var in struct {
				TenantID   string `json:"tenant_id"`
				Tenant     string `json:"tenant"`
				PromptHash string `json:"prompt_hash"`
			}
			if err := json.Unmarshal(body, &in); err != nil {
				writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
				return
			}
			if opts.TenantID == "" {
				opts.TenantID = strings.TrimSpace(in.TenantID)
			}
			if opts.TenantID == "" {
				opts.TenantID = strings.TrimSpace(in.Tenant)
			}
			if opts.PromptHash == "" {
				opts.PromptHash = strings.TrimSpace(in.PromptHash)
			}
		}
	}
	deleted, err := s.llmCache.Flush(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action:       "llm_cache.flush",
		ResourceType: "llm_cache",
		ResourceID:   "response-cache",
		Metadata: map[string]any{
			"tenant_id":    opts.TenantID,
			"prompt_hash":  opts.PromptHash,
			"deleted_rows": deleted,
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"flushed":      true,
		"deleted_rows": deleted,
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
	appmetrics.ObserveLLMRequest(payload.TenantID, payload.Model, payload.StatusCode, payload.TTFTSeconds)
	if s.hooks == nil {
		return
	}
	hookCtx := context.WithoutCancel(ctx)
	if _, err := s.hooks.Fire(hookCtx, hooks.HookLLMPostCall, payload); err != nil {
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
		Modality:   modalityFromOperation(operation),
	}
	s.fireLLMPostCallBest(context.Background(), post)
}

// modalityFromOperation maps the LLM gateway operation tag (set on
// every pre/post-call payload) to its canonical modality string. Used
// by the rejection helpers that don't have the pre-call payload to
// copy from.
func modalityFromOperation(op string) string {
	switch op {
	case "embeddings":
		return ModalityEmbedding
	case "audio_speech":
		return ModalityAudioSpeech
	case "audio_transcriptions":
		return ModalityAudioTranscription
	case "audio_translations":
		return ModalityAudioTranslation
	case "images", "images_edits", "images_variations":
		return ModalityImage
	default:
		return ModalityText
	}
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

func isGuardrailError(err error) bool {
	return errors.Is(err, guardrails.ErrContentBlocked) ||
		errors.Is(err, guardrails.ErrGuardrailsUnavailable)
}

func classifyGuardrailError(err error) (string, string, int) {
	switch {
	case errors.Is(err, guardrails.ErrContentBlocked):
		return guardrails.CodeContentBlocked, "content blocked by gateway moderation policy", http.StatusUnprocessableEntity
	case errors.Is(err, guardrails.ErrGuardrailsUnavailable):
		return guardrails.CodeGuardrailsUnavailable, "gateway guardrails provider unavailable", http.StatusServiceUnavailable
	default:
		return "", "", 0
	}
}

func writeGuardrailError(w http.ResponseWriter, err error) {
	code, msg, status := classifyGuardrailError(err)
	if code == "" {
		code = "POLICY_VIOLATION"
		msg = err.Error()
		status = http.StatusForbidden
	}
	writeOpenAIError(w, status, code, msg, nil)
}

func (s *Server) writeGuardrailRejection(
	w http.ResponseWriter,
	err error,
	requestID string,
	start time.Time,
	tenantID, apiKeyID, model, operation string,
	stream bool,
) {
	writeGuardrailError(w, err)
	post := LLMPostCallPayload{
		TenantID:   tenantID,
		APIKeyID:   apiKeyID,
		Model:      model,
		Provider:   s.llmGateway.ProviderName(),
		RequestID:  requestID,
		Operation:  operation,
		Stream:     stream,
		LatencyMS:  int(time.Since(start).Milliseconds()),
		StatusCode: guardrailStatus(err),
		ErrorCode:  guardrailCode(err),
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		Modality:   modalityFromOperation(operation),
	}
	s.fireLLMPostCallBest(context.Background(), post)
}

func (s *Server) guardrailPost(pre LLMPreCallPayload, err error, start time.Time) LLMPostCallPayload {
	return LLMPostCallPayload{
		TenantID:   pre.TenantID,
		APIKeyID:   pre.APIKeyID,
		Model:      pre.Model,
		Provider:   pre.Provider,
		RequestID:  pre.RequestID,
		Operation:  pre.Operation,
		Stream:     pre.Stream,
		LatencyMS:  int(time.Since(start).Milliseconds()),
		StatusCode: guardrailStatus(err),
		ErrorCode:  guardrailCode(err),
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		Modality:   normalizeModality(pre.Modality),
	}
}

// normalizeModality returns a non-empty modality string, defaulting to
// "text" when the caller didn't set one (chat/embeddings paths).
func normalizeModality(m string) string {
	if m == "" {
		return ModalityText
	}
	return m
}

func guardrailCode(err error) string {
	code, _, _ := classifyGuardrailError(err)
	if code == "" {
		return "POLICY_VIOLATION"
	}
	return code
}

func guardrailStatus(err error) int {
	_, _, status := classifyGuardrailError(err)
	if status == 0 {
		return http.StatusForbidden
	}
	return status
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
		Modality:   normalizeModality(pre.Modality),
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
		if u.ResponseCostUSD != nil {
			post.CostUSD = *u.ResponseCostUSD
			post.CostKnown = true
		} else if cost, ok := s.llmGateway.EstimateCostUSD(pre.Model, u.PromptTokens, u.CompletionTokens); ok {
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
		Modality:   ModalityEmbedding,
	}
	if resp.Usage != nil {
		post.PromptTokens = resp.Usage.PromptTokens
		post.CompletionTokens = resp.Usage.CompletionTokens
		post.TotalTokens = resp.Usage.TotalTokens
		if resp.Usage.ResponseCostUSD != nil {
			post.CostUSD = *resp.Usage.ResponseCostUSD
			post.CostKnown = true
		} else if cost, ok := s.llmGateway.EstimateCostUSD(pre.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens); ok {
			post.CostUSD = cost
			post.CostKnown = true
		}
	}
	return post
}

func (s *Server) simplePostPayload(pre LLMPreCallPayload, start time.Time, status int) LLMPostCallPayload {
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
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		Modality:   normalizeModality(pre.Modality),
	}
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
		Modality:   normalizeModality(pre.Modality),
	}
}
