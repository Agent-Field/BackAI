// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
)

// AFProvider routes every LLM call through AgentField.
//
// This is the canonical implementation of ProviderClient — the one the
// public Phase-7 contract names. It calls a built-in AF reasoner
// (e.g. "__llm.chat") via the AF execute endpoint, which internally
// drives AF's LiteLLM-backed LLM gateway and emits the same response
// shape AgentField returns to its own agents.
//
// Phase 7 status: not yet active in the docker-compose stack.
// AgentField does not yet ship a built-in `__llm.chat` reasoner that
// returns OpenAI-format. main.go therefore wires OpenAICompatProvider
// today; the moment AF lands the reasoner, change main.go to
// construct AFProvider instead. The Gateway, handlers, tests, and
// public contract require zero changes.
//
// Endpoint expectations (when the AF handler lands):
//
//   - Chat:     POST /api/v1/execute/<ChatReasoner>            (sync)
//                 body matches ChatRequest verbatim,
//                 response matches ChatResponse verbatim.
//   - Stream:   POST /api/v1/execute/<ChatReasoner>?stream=1   (SSE)
//                 server emits OpenAI-format `data: {...}\n\n` chunks.
//   - Embed:    POST /api/v1/execute/<EmbeddingsReasoner>
//   - Images:   POST /api/v1/execute/<ImagesReasoner>
//
// AF's chosen reasoner names are configurable here so the AF team can
// pick whatever feels natural (`__llm.chat`, `llm.openai_chat`, etc.).
type AFProvider struct {
	cfg AFProviderConfig
	af  *agentfield.Client
}

// AFProviderConfig configures the AF-routed provider.
type AFProviderConfig struct {
	// ChatReasoner is the AF reasoner id called for /chat/completions
	// (default "__llm.chat"). The leading "/api/v1/execute/" prefix is
	// added automatically.
	ChatReasoner string
	// EmbeddingsReasoner is the AF reasoner id for /embeddings
	// (default "__llm.embeddings").
	EmbeddingsReasoner string
	// ImagesReasoner is the AF reasoner id for /images/generations
	// (default "__llm.images").
	ImagesReasoner string
}

// NewAFProvider constructs an AF-routed provider. The AF client MUST
// be non-nil; if nil, every call returns ErrNoProvider.
func NewAFProvider(af *agentfield.Client, cfg AFProviderConfig) *AFProvider {
	if cfg.ChatReasoner == "" {
		cfg.ChatReasoner = "__llm.chat"
	}
	if cfg.EmbeddingsReasoner == "" {
		cfg.EmbeddingsReasoner = "__llm.embeddings"
	}
	if cfg.ImagesReasoner == "" {
		cfg.ImagesReasoner = "__llm.images"
	}
	return &AFProvider{af: af, cfg: cfg}
}

// Name returns the provider id used in cost-event labelling.
func (p *AFProvider) Name() string { return "agentfield" }

// Chat performs a non-streaming chat completion via AF.
func (p *AFProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if p == nil || p.af == nil {
		return ChatResponse{}, ErrNoProvider
	}
	body, err := json.Marshal(req)
	if err != nil {
		return ChatResponse{}, &APIError{
			Code: ErrCodeInvalidRequest, Message: "encode chat request: " + err.Error(),
		}
	}
	resp, err := p.af.Execute(ctx, "/api/v1/execute/"+p.cfg.ChatReasoner, body)
	if err != nil {
		return ChatResponse{}, &APIError{
			Code:    ErrCodeUpstreamError,
			Message: fmt.Sprintf("agentfield execute: %v", err),
			Status:  http.StatusBadGateway,
		}
	}
	if resp.StatusCode >= 400 {
		return ChatResponse{}, &APIError{
			Code:    ErrCodeUpstreamError,
			Message: extractAFMessage(resp.Body, resp.StatusCode),
			Status:  resp.StatusCode,
			Details: map[string]any{"upstream_body_preview": previewBody(resp.Body)},
		}
	}
	var out ChatResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return ChatResponse{}, &APIError{
			Code:    ErrCodeUpstreamError,
			Message: "decode agentfield response: " + err.Error(),
			Status:  http.StatusBadGateway,
			Details: map[string]any{"upstream_body_preview": previewBody(resp.Body)},
		}
	}
	return out, nil
}

// ChatStream is intentionally NOT YET IMPLEMENTED for AFProvider.
//
// AF's reasoner-execute endpoint is currently sync-only. When AF adds
// SSE support to a `__llm.chat` reasoner, parse the stream the same
// way OpenAICompatProvider does — until then, returning an error here
// surfaces a clear "feature unavailable" rather than silently
// falling back.
func (p *AFProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatStreamChunk, <-chan error) {
	chunkCh := make(chan ChatStreamChunk)
	errCh := make(chan error, 1)
	errCh <- &APIError{
		Code:    "STREAMING_NOT_SUPPORTED",
		Message: "agentfield-routed streaming chat is not yet implemented; set stream=false",
		Status:  http.StatusBadRequest,
	}
	close(chunkCh)
	close(errCh)
	return chunkCh, errCh
}

// Embeddings calls the AF embeddings reasoner.
func (p *AFProvider) Embeddings(ctx context.Context, req EmbeddingsRequest) (EmbeddingsResponse, error) {
	if p == nil || p.af == nil {
		return EmbeddingsResponse{}, ErrNoProvider
	}
	body, _ := json.Marshal(req)
	resp, err := p.af.Execute(ctx, "/api/v1/execute/"+p.cfg.EmbeddingsReasoner, body)
	if err != nil {
		return EmbeddingsResponse{}, &APIError{
			Code:    ErrCodeUpstreamError,
			Message: fmt.Sprintf("agentfield execute: %v", err),
			Status:  http.StatusBadGateway,
		}
	}
	if resp.StatusCode >= 400 {
		return EmbeddingsResponse{}, &APIError{
			Code: ErrCodeUpstreamError, Message: extractAFMessage(resp.Body, resp.StatusCode),
			Status: resp.StatusCode,
		}
	}
	var out EmbeddingsResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return EmbeddingsResponse{}, &APIError{
			Code: ErrCodeUpstreamError, Message: "decode embeddings: " + err.Error(),
		}
	}
	return out, nil
}

// Images calls the AF images reasoner.
func (p *AFProvider) Images(ctx context.Context, req ImagesRequest) (ImagesResponse, error) {
	if p == nil || p.af == nil {
		return ImagesResponse{}, ErrNoProvider
	}
	body, _ := json.Marshal(req)
	resp, err := p.af.Execute(ctx, "/api/v1/execute/"+p.cfg.ImagesReasoner, body)
	if err != nil {
		return ImagesResponse{}, &APIError{
			Code:    ErrCodeUpstreamError,
			Message: fmt.Sprintf("agentfield execute: %v", err),
			Status:  http.StatusBadGateway,
		}
	}
	if resp.StatusCode >= 400 {
		return ImagesResponse{}, &APIError{
			Code: ErrCodeUpstreamError, Message: extractAFMessage(resp.Body, resp.StatusCode),
			Status: resp.StatusCode,
		}
	}
	var out ImagesResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return ImagesResponse{}, &APIError{
			Code: ErrCodeUpstreamError, Message: "decode images: " + err.Error(),
		}
	}
	return out, nil
}

// extractAFMessage tries to pull a useful error.message out of an
// AgentField error response body. AF uses the same error envelope as
// this runtime (`{error:{code,message}}`), so the structural extractor
// works for both.
func extractAFMessage(body []byte, status int) string {
	if len(body) == 0 {
		return fmt.Sprintf("agentfield returned status %d", status)
	}
	var aux struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &aux); err == nil {
		switch {
		case aux.Error.Message != "":
			if aux.Error.Code != "" {
				return fmt.Sprintf("%s: %s", aux.Error.Code, aux.Error.Message)
			}
			return aux.Error.Message
		case aux.Detail != "":
			return aux.Detail
		}
	}
	// Body wasn't JSON or didn't match — return a trimmed preview so
	// the caller can see what AF actually said.
	return fmt.Sprintf("agentfield status %d: %s", status, previewBody(body))
}

// Compile-time guard that strings is in use (used by previewBody
// indirectly via fmt). Keeps goimports quiet if we ever trim helpers.
var _ = strings.TrimSpace