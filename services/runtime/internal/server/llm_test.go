// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/guardrails"
	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
	litellmadapter "github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters/litellm"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
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
	provider := llmgateway.NewLiteLLMProvider(llmgateway.LiteLLMConfig{
		ProviderID: providerID,
		BaseURL:    upstreamURL,
		MasterKey:  "test-key",
	})
	// Wire a multimodal facade with the LiteLLM-backed fallback so the
	// translations + image-edit/variation paths can dispatch through
	// the same provider in tests (production wires the same fallback
	// from main.go).
	fallback := litellmadapter.New(provider, "openai/whisper-1", "openai/tts-1",
		"openai/tts-1-hd", "openai/dall-e-2", "openai/dall-e-3", "openai/gpt-image-1")
	mm := llmgateway.NewMultimodal(adapters.NewRegistry(), fallback)
	return llmgateway.New(provider).WithMultimodal(mm)
}

type llmTestSecretSink struct {
	values map[string]string
}

func (s *llmTestSecretSink) Put(ctx context.Context, tenantID, key string, in tenancy.SecretPutInput) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[tenantID+"|"+key] = in.Value
	return nil
}

func (s *llmTestSecretSink) Delete(ctx context.Context, tenantID, key string) error {
	delete(s.values, tenantID+"|"+key)
	return nil
}

func (s *llmTestSecretSink) Get(ctx context.Context, tenantID, key string) ([]byte, error) {
	if v, ok := s.values[tenantID+"|"+key]; ok {
		return []byte(v), nil
	}
	return nil, tenancy.ErrAPIKeyNotFound
}

func TestAttachLiteLLMKeyUsesTenantVirtualKey(t *testing.T) {
	seenAuth := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-virtual-key","object":"chat.completion","created":1,"model":"m",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer upstream.Close()

	sink := &llmTestSecretSink{values: map[string]string{}}
	if err := sink.Put(context.Background(), "tenant-1", tenancy.LiteLLMSecretKey("api-key-1"), tenancy.SecretPutInput{
		Value: "sk-litellm-tenant-virtual",
	}); err != nil {
		t.Fatal(err)
	}
	mgr := (&tenancy.Manager{}).WithLiteLLM(nil, sink)
	srv := newLLMTestServer(t, Deps{
		Tenancy:    mgr,
		LLMGateway: gatewayFor(upstream.URL, "litellm"),
	})
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1", "api-key-1")
	_, err := srv.llmGateway.Chat(srv.attachLiteLLMKey(ctx), llmgateway.ChatRequest{
		Model:    "m",
		Messages: []llmgateway.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if seenAuth != "Bearer sk-litellm-tenant-virtual" {
		t.Fatalf("Authorization = %q, want tenant virtual key", seenAuth)
	}
}

func TestBuildPostPayloadPrefersProviderResponseCost(t *testing.T) {
	srv := newLLMTestServer(t, Deps{
		LLMGateway: llmgateway.New(llmgateway.NewDemoProvider()),
	})
	providerCost := 0.00123
	post := srv.buildPostPayload(
		LLMPreCallPayload{
			TenantID:  "tenant-1",
			APIKeyID:  "key-1",
			Model:     "demo/supportdesk",
			Provider:  "demo",
			RequestID: "req-demo",
			Operation: "chat",
		},
		&llmgateway.ChatResponse{
			Usage: &llmgateway.Usage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
				ResponseCostUSD:  &providerCost,
			},
		},
		nil,
		time.Now(),
		http.StatusOK,
	)
	if !post.CostKnown {
		t.Fatalf("expected provider response cost to mark cost known")
	}
	if post.CostUSD != providerCost {
		t.Fatalf("cost = %v, want %v", post.CostUSD, providerCost)
	}
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

// TestLLMChat429PassesThroughHeaders is item #32's signature integration
// test: LiteLLM enforces per-virtual-key RPM upstream (item #22) and the
// runtime no longer runs a local token-bucket. When LiteLLM returns 429,
// the runtime MUST proxy Retry-After + the X-RateLimit-* trio through to
// the client unchanged so SDK consumers (openai-python's RateLimitError,
// etc.) see the standard signal.
//
// Also verifies the error envelope:
//   - status code 429,
//   - code "RATE_LIMIT_EXCEEDED" (renamed from MODEL_RATE_LIMITED with #32),
//   - error.type "rate_limit_error" (openai-python branches on this),
//   - details.retry_after parsed from Retry-After for clients that
//     prefer JSON over headers.
func TestLLMChat429PassesThroughHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.Header().Set("X-RateLimit-Limit", "60")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit exceeded: rpm cap reached"}}`))
	}))
	defer upstream.Close()
	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "litellm")})

	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", rec.Code, rec.Body.String())
	}

	// Rate-limit headers must reach the client verbatim — that's the
	// whole point of item #32 (kill the local limiter, let LiteLLM speak).
	for name, want := range map[string]string{
		"Retry-After":           "30",
		"X-RateLimit-Limit":     "60",
		"X-RateLimit-Remaining": "0",
		"X-RateLimit-Reset":     "1700000000",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}

	// Error envelope shape: code RATE_LIMIT_EXCEEDED, type rate_limit_error,
	// details.retry_after populated from Retry-After header.
	var env struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Type    string         `json:"type"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body not JSON: %v body=%s", err, rec.Body.String())
	}
	if env.Error.Code != "RATE_LIMIT_EXCEEDED" {
		t.Errorf("error.code = %q, want RATE_LIMIT_EXCEEDED", env.Error.Code)
	}
	if env.Error.Type != "rate_limit_error" {
		t.Errorf("error.type = %q, want rate_limit_error", env.Error.Type)
	}
	if env.Error.Message == "" {
		t.Errorf("error.message empty")
	}
	if env.Error.Details["retry_after"] == nil {
		t.Errorf("details.retry_after missing; details=%v", env.Error.Details)
	} else if got := env.Error.Details["retry_after"]; got != float64(30) {
		t.Errorf("details.retry_after = %v, want 30", got)
	}
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

func TestLLMChatStreamingEstimatesUsageWhenUpstreamOmitsUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f := w.(http.Flusher)
		fmt.Fprint(w, `data: {"id":"c","object":"chat.completion.chunk","model":"qwen/qwen-2.5-72b-instruct","choices":[{"index":0,"delta":{"content":"Hello customer"}}]}`+"\n\n")
		f.Flush()
		fmt.Fprint(w, `data: {"id":"c","object":"chat.completion.chunk","model":"qwen/qwen-2.5-72b-instruct","choices":[{"index":0,"delta":{"content":"."},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		f.Flush()
	}))
	defer upstream.Close()

	var lastPost LLMPostCallPayload
	eng := hooks.NewEngine(slog.Default())
	_ = eng.Register(hooks.HookLLMPostCall, func(_ context.Context, p any) (any, error) {
		lastPost = p.(LLMPostCallPayload)
		return p, nil
	})
	srv := newLLMTestServer(t, Deps{
		LLMGateway: gatewayFor(upstream.URL, "litellm"),
		Hooks:      eng,
	})

	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{"model":"qwen/qwen-2.5-72b-instruct","messages":[{"role":"user","content":"Please draft a refund response for invoice INV-8842."}],"stream":true}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if lastPost.PromptTokens <= 0 || lastPost.CompletionTokens <= 0 || lastPost.TotalTokens <= 0 {
		t.Fatalf("expected estimated usage, got %+v", lastPost)
	}
	if !lastPost.CostKnown || lastPost.CostUSD <= 0 {
		t.Fatalf("expected estimated cost, got %+v", lastPost)
	}
}

// ─── embeddings ───────────────────────────────────────────────────────────

func TestLLMEmbeddingsHappyPath(t *testing.T) {
	body := `{"object":"list","model":"m","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`
	upstream := upstreamChatServer(t, body, http.StatusOK)
	defer upstream.Close()

	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	for _, path := range []string{"/api/v1/embeddings", "/api/v1/llm/embeddings"} {
		req := httptest.NewRequest("POST", path,
			strings.NewReader(`{"model":"m","input":"hi"}`))
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d body=%s", path, rec.Code, rec.Body.String())
		}
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
	for _, path := range []string{"/api/v1/images/generations", "/api/v1/llm/images/generations"} {
		req := httptest.NewRequest("POST", path,
			strings.NewReader(`{"prompt":"a cat"}`))
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d body=%s", path, rec.Code, rec.Body.String())
		}
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

func TestLLMAudioSpeechHappyPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/speech" {
			t.Errorf("unexpected upstream path: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("mp3 bytes"))
	}))
	defer upstream.Close()

	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/audio/speech",
		strings.NewReader(`{"model":"tts-1","input":"hello","voice":"alloy"}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "audio/mpeg" {
		t.Errorf("expected audio content-type, got %q", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "mp3 bytes" {
		t.Errorf("expected raw audio bytes, got %q", rec.Body.String())
	}
}

func TestLLMAudioSpeechMissingInput(t *testing.T) {
	upstream := upstreamChatServer(t, `{}`, http.StatusOK)
	defer upstream.Close()
	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/audio/speech",
		strings.NewReader(`{"model":"tts-1"}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestLLMAudioTranscriptionHappyPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			t.Errorf("unexpected upstream path: %q", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
			t.Errorf("expected multipart content-type, got %q", ct)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello world"}`))
	}))
	defer upstream.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("model", "whisper-1"); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("file", "audio.mp3")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("audio bytes"))
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/audio/transcriptions", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"text":"hello world"}` {
		t.Errorf("expected transcription JSON passthrough, got %s", rec.Body.String())
	}
}

func TestLLMAudioTranscriptionMissingFile(t *testing.T) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("model", "whisper-1"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	upstream := upstreamChatServer(t, `{}`, http.StatusOK)
	defer upstream.Close()
	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/audio/transcriptions", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
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
	// cache/stats is operator-gated (it exposes cross-tenant cache metrics);
	// authorize the request so the test exercises the zero-cache behavior.
	withOperator(srv, "owner")
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

func TestLLMPostHookIgnoresRequestCancellation(t *testing.T) {
	eng := hooks.NewEngine(slog.Default())
	var sawTenant string
	var sawCanceled bool
	_ = eng.Register(hooks.HookLLMPostCall, func(ctx context.Context, p any) (any, error) {
		sawTenant = tenantctx.TenantID(ctx)
		sawCanceled = ctx.Err() != nil
		return p, nil
	})

	srv := newLLMTestServer(t, Deps{Hooks: eng})
	ctx, cancel := context.WithCancel(tenantctx.WithTenant(context.Background(), "tenant-post", "key-post"))
	cancel()

	srv.fireLLMPostCallBest(ctx, LLMPostCallPayload{
		TenantID:  "tenant-post",
		Model:     "m",
		Provider:  "litellm",
		RequestID: "req-post",
		Operation: "chat",
	})

	if sawCanceled {
		t.Fatal("post-call hook saw canceled context")
	}
	if sawTenant != "tenant-post" {
		t.Fatalf("tenant context = %q, want tenant-post", sawTenant)
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

func TestLLMGuardrailsRedactsChatBeforeUpstream(t *testing.T) {
	var upstreamBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-guard","object":"chat.completion","created":1,"model":"m",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}
		}`))
	}))
	defer upstream.Close()

	gr, err := guardrails.New(guardrails.Config{Enabled: true, RedactPII: true, Moderate: true}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	srv := newLLMTestServer(t, Deps{
		LLMGateway: gatewayFor(upstream.URL, "litellm"),
		Guardrails: gr,
	})
	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"email alice@example.com"}]}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	encoded, _ := json.Marshal(upstreamBody)
	if strings.Contains(string(encoded), "alice@example.com") {
		t.Fatalf("upstream received unredacted PII: %s", string(encoded))
	}
	if !strings.Contains(string(encoded), "[REDACTED_EMAIL_ADDRESS]") {
		t.Fatalf("upstream did not receive redaction marker: %s", string(encoded))
	}
}

func TestLLMGuardrailsModerationBlocksBeforeUpstream(t *testing.T) {
	upstreamCalled := int32(0)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&upstreamCalled, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	gr, err := guardrails.New(guardrails.Config{
		Enabled:       true,
		RedactPII:     true,
		Moderate:      true,
		BlockPatterns: []string{`(?i)blocked topic`},
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	srv := newLLMTestServer(t, Deps{
		LLMGateway: gatewayFor(upstream.URL, "litellm"),
		Guardrails: gr,
	})
	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"blocked topic"}]}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertOpenAIErrorCode(t, rec.Body.Bytes(), guardrails.CodeContentBlocked)
	if atomic.LoadInt32(&upstreamCalled) != 0 {
		t.Fatal("upstream should not be called for moderated prompt")
	}
}

func TestLLMGuardrailsRedactsChatResponse(t *testing.T) {
	upstream := upstreamChatServer(t, `{
		"id":"chatcmpl-guard-response","object":"chat.completion","created":1,"model":"m",
		"choices":[{"index":0,"message":{"role":"assistant","content":"send to bob@example.com"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}
	}`, http.StatusOK)
	defer upstream.Close()

	gr, err := guardrails.New(guardrails.Config{Enabled: true, RedactPII: true, Moderate: true}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	srv := newLLMTestServer(t, Deps{
		LLMGateway: gatewayFor(upstream.URL, "litellm"),
		Guardrails: gr,
	})
	req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "bob@example.com") {
		t.Fatalf("client received unredacted PII: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "[REDACTED_EMAIL_ADDRESS]") {
		t.Fatalf("client did not receive redaction marker: %s", rec.Body.String())
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
		"/api/v1/embeddings",
		"/api/v1/llm/embeddings",
		"/api/v1/images/generations",
		"/api/v1/llm/images/generations",
		"/api/v1/audio/speech",
		"/api/v1/audio/transcriptions",
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

// ─── #14 — Multimodal: translations + image edits + variations + modality ───

func TestLLMAudioTranslationsHappyPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/translations" {
			t.Errorf("unexpected upstream path: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello (translated)"}`))
	}))
	defer upstream.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("model", "openai/whisper-1"); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("file", "audio.mp3")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("french audio"))
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/audio/translations", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "translated") {
		t.Errorf("expected translated text passthrough, got %s", rec.Body.String())
	}
}

func TestLLMImageEditsHappyPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			t.Errorf("unexpected upstream path: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1234,"data":[{"url":"https://example.com/a.png"}]}`))
	}))
	defer upstream.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("model", "openai/dall-e-2")
	_ = mw.WriteField("prompt", "add a hat")
	part, _ := mw.CreateFormFile("image", "image.png")
	_, _ = part.Write([]byte("\x89PNGfakeimage"))
	_ = mw.Close()

	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/images/edits", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "https://example.com/a.png") {
		t.Errorf("expected image URL in response, got %s", rec.Body.String())
	}
}

func TestLLMImageEditsMissingImage(t *testing.T) {
	upstream := upstreamChatServer(t, `{}`, http.StatusOK)
	defer upstream.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("model", "openai/dall-e-2")
	_ = mw.WriteField("prompt", "x")
	_ = mw.Close()

	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/images/edits", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLLMImageVariationsHappyPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/variations" {
			t.Errorf("unexpected upstream path: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1234,"data":[{"url":"https://example.com/v.png"}]}`))
	}))
	defer upstream.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("model", "openai/dall-e-2")
	_ = mw.WriteField("n", "2")
	part, _ := mw.CreateFormFile("image", "image.png")
	_, _ = part.Write([]byte("\x89PNGfakeimage"))
	_ = mw.Close()

	srv := newLLMTestServer(t, Deps{LLMGateway: gatewayFor(upstream.URL, "openrouter")})
	req := httptest.NewRequest("POST", "/api/v1/images/variations", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "https://example.com/v.png") {
		t.Errorf("expected variation URL in response, got %s", rec.Body.String())
	}
}

// TestLLMSpeechCarriesModalityToHook verifies that the multimodal pre/post
// hook payloads carry modality=audio_speech (item #14 cost tracking).
func TestLLMSpeechCarriesModalityToHook(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("mp3"))
	}))
	defer upstream.Close()

	var modSeen string
	var mu sync.Mutex
	engine := hooks.NewEngine(slog.Default())
	engine.Register(hooks.HookLLMPostCall, func(_ context.Context, payload any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		if p, ok := payload.(LLMPostCallPayload); ok {
			modSeen = p.Modality
		}
		return payload, nil
	})

	srv := newLLMTestServer(t, Deps{
		LLMGateway: gatewayFor(upstream.URL, "openrouter"),
		Hooks:      engine,
	})
	req := httptest.NewRequest("POST", "/api/v1/audio/speech",
		strings.NewReader(`{"model":"openai/tts-1","input":"hi","voice":"alloy"}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if modSeen != ModalityAudioSpeech {
		t.Errorf("expected post-call modality=%q, got %q", ModalityAudioSpeech, modSeen)
	}
}
