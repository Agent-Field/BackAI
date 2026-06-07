// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
)

// ─── helpers ──────────────────────────────────────────────────────────────

func newLLMTestServer(t *testing.T, deps Deps) *Server {
	t.Helper()
	cfg := config.Default()
	return New(cfg, slog.Default(), deps)
}

// upstreamChatServer returns an httptest.Server that mimics an
// OpenAI-compatible /chat/completions endpoint.
func upstreamChatServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" && r.URL.Path != "/embeddings" && r.URL.Path != "/images/generations" {
			t.Errorf("unexpected upstream path: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func gatewayFor(upstreamURL, providerID string) *llmgateway.Gateway {
	return llmgateway.New(llmgateway.NewOpenAICompatProvider(llmgateway.OpenAICompatConfig{
		ProviderID: providerID,
		BaseURL:    upstreamURL,
		APIKey:     "test-key",
	}))
}

// ─── chat/completions ─────────────────────────────────────────────────────

func TestLLMChatHappyPath(t *testing.T) {
	body := `{
		"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"openrouter/qwen/qwen-2.5-7b-instruct",
		"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}
	}`
	upstream := upstreamChatServer(t, body, http.StatusOK)
	defer upstream.Close()

	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})

	reqBody := `{"model":"openrouter/qwen/qwen-2.5-7b-instruct","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["id"] != "chatcmpl-1" {
		t.Errorf("expected id passthrough, got %v", out["id"])
	}
}

func TestLLMChatGatewayNotConfigured(t *testing.T) {
	srv := newLLMTestServer(t, Deps{})
	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"x"}]}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	assertOpenAIErrorCode(t, rec.Body.Bytes(), "GATEWAY_NOT_CONFIGURED")
}

func TestLLMChatMissingModel(t *testing.T) {
	upstream := upstreamChatServer(t, `{}`, http.StatusOK)
	defer upstream.Close()
	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"x"}]}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertOpenAIErrorCode(t, rec.Body.Bytes(), llmgateway.ErrCodeInvalidRequest)
}

func TestLLMChatMissingMessages(t *testing.T) {
	upstream := upstreamChatServer(t, `{}`, http.StatusOK)
	defer upstream.Close()
	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{"model":"m"}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLLMChatInvalidJSON(t *testing.T) {
	upstream := upstreamChatServer(t, `{}`, http.StatusOK)
	defer upstream.Close()
	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{not json`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestLLMChatUpstream5xxBecomes502(t *testing.T) {
	upstream := upstreamChatServer(t, `{"error":{"message":"boom"}}`, http.StatusBadGateway)
	defer upstream.Close()
	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"x"}]}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertOpenAIErrorCode(t, rec.Body.Bytes(), llmgateway.ErrCodeUpstreamError)
}

// ─── streaming ────────────────────────────────────────────────────────────

func TestLLMChatStreamingForwardsAndTerminates(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f := w.(http.Flusher)
		emit := func(p string) {
			fmt.Fprintf(w, "data: %s\n\n", p)
			f.Flush()
		}
		emit(`{"id":"c","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}`)
		emit(`{"id":"c","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":" there"}}]}`)
		emit(`{"id":"c","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
		f.Flush()
	}))
	defer upstream.Close()

	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})

	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Errorf("expected [DONE] terminator, got tail=%q", body[max(0, len(body)-32):])
	}
	if !strings.Contains(body, `"content":"hi"`) {
		t.Errorf("expected first content chunk to be forwarded, body=%s", body)
	}
	if !strings.Contains(body, `"content":" there"`) {
		t.Errorf("expected second content chunk to be forwarded, body=%s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected SSE content-type, got %q", ct)
	}
}

// ─── embeddings ───────────────────────────────────────────────────────────

func TestLLMEmbeddingsHappyPath(t *testing.T) {
	body := `{"object":"list","model":"m","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`
	upstream := upstreamChatServer(t, body, http.StatusOK)
	defer upstream.Close()

	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/llm/embeddings",
		strings.NewReader(`{"model":"m","input":"hi"}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLLMEmbeddingsMissingModel(t *testing.T) {
	upstream := upstreamChatServer(t, `{}`, http.StatusOK)
	defer upstream.Close()
	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/llm/embeddings",
		strings.NewReader(`{"input":"hi"}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// ─── images ───────────────────────────────────────────────────────────────

func TestLLMImagesHappyPath(t *testing.T) {
	body := `{"created":1,"data":[{"url":"https://x"}]}`
	upstream := upstreamChatServer(t, body, http.StatusOK)
	defer upstream.Close()

	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/llm/images/generations",
		strings.NewReader(`{"prompt":"a cat"}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLLMImagesMissingPrompt(t *testing.T) {
	upstream := upstreamChatServer(t, `{}`, http.StatusOK)
	defer upstream.Close()
	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/llm/images/generations",
		strings.NewReader(`{"model":"dall-e-3"}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// ─── models + cache stats ────────────────────────────────────────────────

func TestLLMModelsReturnsCatalog(t *testing.T) {
	srv := newLLMTestServer(t, Deps{})
	req := httptest.NewRequest("GET", "/api/v1/llm/models", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out llmgateway.LLMModelList
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Models) == 0 {
		t.Fatal("expected non-empty model list")
	}
	for _, m := range out.Models {
		if m.ID == "" || m.Provider == "" {
			t.Errorf("expected populated id+provider, got %+v", m)
		}
	}
}

func TestLLMCacheStatsZeroesWhenUnwired(t *testing.T) {
	srv := newLLMTestServer(t, Deps{})
	req := httptest.NewRequest("GET", "/api/v1/llm/cache/stats", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out llmCacheStats
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.TotalCalls != 0 || out.Entries != 0 {
		t.Errorf("expected zeros without cache, got %+v", out)
	}
}

// ─── hook firing ─────────────────────────────────────────────────────────

func TestLLMPreAndPostHooksFire(t *testing.T) {
	body := `{
		"id":"chatcmpl-2","object":"chat.completion","created":1,"model":"openrouter/qwen/qwen-2.5-7b-instruct",
		"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`
	upstream := upstreamChatServer(t, body, http.StatusOK)
	defer upstream.Close()

	preCount := int32(0)
	postCount := int32(0)
	var lastPre LLMPreCallPayload
	var lastPost LLMPostCallPayload
	var mu sync.Mutex

	eng := hooks.NewEngine(slog.Default())
	_ = eng.Register(hooks.HookLLMPreCall, func(_ context.Context, p any) (any, error) {
		atomic.AddInt32(&preCount, 1)
		mu.Lock()
		lastPre = p.(LLMPreCallPayload)
		mu.Unlock()
		return p, nil
	})
	_ = eng.Register(hooks.HookLLMPostCall, func(_ context.Context, p any) (any, error) {
		atomic.AddInt32(&postCount, 1)
		mu.Lock()
		lastPost = p.(LLMPostCallPayload)
		mu.Unlock()
		return p, nil
	})

	srv := newLLMTestServer(t, Deps{
		LLMGateway: gatewayFor(upstream.URL, "openrouter"),
		Hooks:      eng,
	})

	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{"model":"openrouter/qwen/qwen-2.5-7b-instruct","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if atomic.LoadInt32(&preCount) != 1 {
		t.Errorf("expected pre-hook fired once, got %d", preCount)
	}
	if atomic.LoadInt32(&postCount) != 1 {
		t.Errorf("expected post-hook fired once, got %d", postCount)
	}
	mu.Lock()
	defer mu.Unlock()
	if lastPre.Operation != "chat" {
		t.Errorf("expected operation=chat, got %q", lastPre.Operation)
	}
	if lastPre.Provider != "openrouter" {
		t.Errorf("expected provider=openrouter, got %q", lastPre.Provider)
	}
	if lastPost.PromptTokens != 10 || lastPost.CompletionTokens != 5 || lastPost.TotalTokens != 15 {
		t.Errorf("expected usage in post payload, got %+v", lastPost)
	}
	if !lastPost.CostKnown {
		t.Errorf("expected cost_known=true for catalog model")
	}
	if lastPost.CostUSD <= 0 {
		t.Errorf("expected positive cost, got %f", lastPost.CostUSD)
	}
	if lastPost.RequestID != lastPre.RequestID {
		t.Errorf("expected matching request_id, got pre=%q post=%q", lastPre.RequestID, lastPost.RequestID)
	}
}

func TestLLMPreHookCanRejectWithBudgetExceeded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream should not be called when pre-hook rejects")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	eng := hooks.NewEngine(slog.Default())
	_ = eng.Register(hooks.HookLLMPreCall, func(_ context.Context, p any) (any, error) {
		return p, &llmgateway.APIError{
			Code:    llmgateway.ErrCodeBudgetExceeded,
			Message: "monthly cap reached",
			Status:  http.StatusPaymentRequired,
		}
	})
	postFired := int32(0)
	_ = eng.Register(hooks.HookLLMPostCall, func(_ context.Context, p any) (any, error) {
		atomic.AddInt32(&postFired, 1)
		return p, nil
	})

	srv := newLLMTestServer(t, Deps{
		LLMGateway: gatewayFor(upstream.URL, "openrouter"),
		Hooks:      eng,
	})
	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"x"}]}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Errorf("expected 402, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertOpenAIErrorCode(t, rec.Body.Bytes(), llmgateway.ErrCodeBudgetExceeded)
	// Post-hook should still fire so the rejection is audited.
	if atomic.LoadInt32(&postFired) != 1 {
		t.Errorf("expected post-hook fired on rejection, got %d", postFired)
	}
}

// ─── OpenAPI registration ────────────────────────────────────────────────

func TestLLMRoutesAppearInOpenAPI(t *testing.T) {
	srv := newLLMTestServer(t, Deps{})
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, path := range []string{
		"/api/v1/llm/chat/completions",
		"/api/v1/llm/embeddings",
		"/api/v1/llm/images/generations",
		"/api/v1/llm/models",
		"/api/v1/llm/cache/stats",
	} {
		if !strings.Contains(body, path) {
			t.Errorf("expected %q in openapi spec", path)
		}
	}
}

// ─── error envelope shape ────────────────────────────────────────────────

// assertOpenAIErrorCode validates that the body is an OpenAI-format
// error envelope with the expected code. The shape is:
//
//	{"error": {"message": "...", "code": "...", "type": "..."}}
func assertOpenAIErrorCode(t *testing.T, body []byte, wantCode string) {
	t.Helper()
	var env struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("body not JSON: %v body=%s", err, body)
	}
	if env.Error.Code != wantCode {
		t.Errorf("expected error.code=%q, got %q body=%s", wantCode, env.Error.Code, body)
	}
	if env.Error.Type == "" {
		t.Errorf("expected error.type populated, got body=%s", body)
	}
}