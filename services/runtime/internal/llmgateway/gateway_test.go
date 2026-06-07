// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ─── Fake provider for unit-testing the Gateway façade ────────────────────

type fakeProvider struct {
	chatResp  ChatResponse
	chatErr   error
	embedResp EmbeddingsResponse
	embedErr  error
	imgResp   ImagesResponse
	imgErr    error
	chunks    []ChatStreamChunk
	streamErr error
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) Chat(_ context.Context, _ ChatRequest) (ChatResponse, error) {
	return f.chatResp, f.chatErr
}
func (f *fakeProvider) ChatStream(_ context.Context, _ ChatRequest) (<-chan ChatStreamChunk, <-chan error) {
	chunkCh := make(chan ChatStreamChunk, len(f.chunks))
	errCh := make(chan error, 1)
	for _, c := range f.chunks {
		chunkCh <- c
	}
	close(chunkCh)
	if f.streamErr != nil {
		errCh <- f.streamErr
	}
	close(errCh)
	return chunkCh, errCh
}
func (f *fakeProvider) Embeddings(_ context.Context, _ EmbeddingsRequest) (EmbeddingsResponse, error) {
	return f.embedResp, f.embedErr
}
func (f *fakeProvider) Images(_ context.Context, _ ImagesRequest) (ImagesResponse, error) {
	return f.imgResp, f.imgErr
}

// ─── Gateway-level unit tests ─────────────────────────────────────────────

func TestGatewayChatHappyPath(t *testing.T) {
	g := New(&fakeProvider{
		chatResp: ChatResponse{
			ID:    "chatcmpl-1",
			Model: "openrouter/qwen/qwen-2.5-7b-instruct",
			Choices: []ChatChoice{
				{Index: 0, Message: ChatMessage{Role: "assistant", Content: "hi"}, FinishReason: "stop"},
			},
			Usage: &Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
		},
	})
	resp, err := g.Chat(context.Background(), ChatRequest{
		Model:    "openrouter/qwen/qwen-2.5-7b-instruct",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "chatcmpl-1" {
		t.Errorf("expected id passthrough, got %q", resp.ID)
	}
	if resp.Choices[0].Message.Content != "hi" {
		t.Errorf("expected content hi, got %v", resp.Choices[0].Message.Content)
	}
}

func TestGatewayChatValidationMissingModel(t *testing.T) {
	g := New(&fakeProvider{})
	_, err := g.Chat(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != ErrCodeInvalidRequest {
		t.Errorf("expected INVALID_REQUEST, got %q", apiErr.Code)
	}
}

func TestGatewayChatValidationMissingMessages(t *testing.T) {
	g := New(&fakeProvider{})
	_, err := g.Chat(context.Background(), ChatRequest{Model: "m"})
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != ErrCodeInvalidRequest {
		t.Errorf("expected INVALID_REQUEST, got %q", apiErr.Code)
	}
}

func TestGatewayNilProviderReturnsErr(t *testing.T) {
	g := New(nil)
	_, err := g.Chat(context.Background(), ChatRequest{
		Model:    "m",
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestGatewayEmbeddingsHappyPath(t *testing.T) {
	g := New(&fakeProvider{
		embedResp: EmbeddingsResponse{
			Object: "list",
			Data:   []EmbeddingItem{{Object: "embedding", Index: 0, Embedding: []float64{0.1, 0.2}}},
			Model:  "text-embedding-3-small",
			Usage:  &Usage{PromptTokens: 3, TotalTokens: 3},
		},
	})
	resp, err := g.Embeddings(context.Background(), EmbeddingsRequest{
		Model: "text-embedding-3-small",
		Input: "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Errorf("expected 1 embedding, got %d", len(resp.Data))
	}
}

func TestGatewayImagesHappyPath(t *testing.T) {
	g := New(&fakeProvider{
		imgResp: ImagesResponse{Created: 1000, Data: []ImageItem{{URL: "https://x"}}},
	})
	resp, err := g.Images(context.Background(), ImagesRequest{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Data[0].URL != "https://x" {
		t.Errorf("expected URL passthrough, got %q", resp.Data[0].URL)
	}
}

func TestGatewayImagesMissingPrompt(t *testing.T) {
	g := New(&fakeProvider{})
	_, err := g.Images(context.Background(), ImagesRequest{})
	apiErr, ok := AsAPIError(err)
	if !ok || apiErr.Code != ErrCodeInvalidRequest {
		t.Errorf("expected INVALID_REQUEST, got %v", err)
	}
}

func TestGatewayEmbeddingsMissingFields(t *testing.T) {
	g := New(&fakeProvider{})
	_, err := g.Embeddings(context.Background(), EmbeddingsRequest{Input: "x"})
	if apiErr, ok := AsAPIError(err); !ok || apiErr.Code != ErrCodeInvalidRequest {
		t.Errorf("expected INVALID_REQUEST for missing model, got %v", err)
	}
	_, err = g.Embeddings(context.Background(), EmbeddingsRequest{Model: "m"})
	if apiErr, ok := AsAPIError(err); !ok || apiErr.Code != ErrCodeInvalidRequest {
		t.Errorf("expected INVALID_REQUEST for missing input, got %v", err)
	}
}

func TestGatewayEstimateCostKnownModel(t *testing.T) {
	g := New(&fakeProvider{})
	cost, ok := g.EstimateCostUSD("openrouter/qwen/qwen-2.5-7b-instruct", 1_000_000, 1_000_000)
	if !ok {
		t.Fatal("expected catalog hit")
	}
	if cost <= 0 {
		t.Errorf("expected positive cost, got %f", cost)
	}
}

func TestGatewayEstimateCostUnknownModel(t *testing.T) {
	g := New(&fakeProvider{})
	_, ok := g.EstimateCostUSD("vendor/unknown", 100, 100)
	if ok {
		t.Error("expected miss for unknown model")
	}
}

// ─── LiteLLMProvider tests using httptest upstream stubs ─────────────────
//
// Tests stand up a small httptest server that mimics the LiteLLM Proxy
// OpenAI-compatible surface (and its response_cost extension).

func TestLiteLLMProviderChatHappyPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected bearer test-key, got %q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["stream"] != false {
			t.Errorf("expected stream=false for non-stream Chat, got %v", body["stream"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-up","object":"chat.completion","created":1,"model":"x",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}
		}`))
	}))
	defer upstream.Close()

	p := NewLiteLLMProvider(LiteLLMConfig{
		ProviderID: "test", BaseURL: upstream.URL, MasterKey: "test-key",
	})
	resp, err := p.Chat(context.Background(), ChatRequest{
		Model:    "x",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "chatcmpl-up" {
		t.Errorf("expected id passthrough, got %q", resp.ID)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 5 {
		t.Errorf("expected usage passthrough, got %+v", resp.Usage)
	}
}

// LiteLLM injects `response_cost` (USD float) at the top level when its
// config has model pricing. The provider lifts it into Usage so the
// post-call hook can prefer it over the static pricing.EstimateCostUSD
// fallback.
func TestLiteLLMProviderChatResponseCostPassthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-rc","object":"chat.completion","created":1,"model":"x",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15},
			"response_cost":0.0042
		}`))
	}))
	defer upstream.Close()

	p := NewLiteLLMProvider(LiteLLMConfig{BaseURL: upstream.URL})
	resp, err := p.Chat(context.Background(), ChatRequest{
		Model:    "x",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Usage == nil || resp.Usage.ResponseCostUSD == nil {
		t.Fatalf("expected response_cost surfaced into Usage, got %+v", resp.Usage)
	}
	if got := *resp.Usage.ResponseCostUSD; got != 0.0042 {
		t.Errorf("expected response_cost=0.0042, got %f", got)
	}
}

func TestLiteLLMProviderChatUpstream5xx(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream pile-up"}}`))
	}))
	defer upstream.Close()

	p := NewLiteLLMProvider(LiteLLMConfig{BaseURL: upstream.URL})
	_, err := p.Chat(context.Background(), ChatRequest{
		Model: "m", Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.Code != ErrCodeUpstreamError {
		t.Errorf("expected UPSTREAM_ERROR, got %q", apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, "upstream pile-up") {
		t.Errorf("expected upstream message extracted, got %q", apiErr.Message)
	}
}

func TestLiteLLMProviderChat429Maps(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
	defer upstream.Close()

	p := NewLiteLLMProvider(LiteLLMConfig{BaseURL: upstream.URL})
	_, err := p.Chat(context.Background(), ChatRequest{
		Model: "m", Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("expected APIError, got %v", err)
	}
	if apiErr.Code != ErrCodeModelRateLimited {
		t.Errorf("expected MODEL_RATE_LIMITED, got %q", apiErr.Code)
	}
}

func TestLiteLLMProviderChat404Maps(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"no such model"}}`))
	}))
	defer upstream.Close()

	p := NewLiteLLMProvider(LiteLLMConfig{BaseURL: upstream.URL})
	_, err := p.Chat(context.Background(), ChatRequest{
		Model: "m", Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	apiErr, _ := AsAPIError(err)
	if apiErr == nil || apiErr.Code != ErrCodeModelNotSupported {
		t.Errorf("expected MODEL_NOT_SUPPORTED, got %v", err)
	}
}

func TestLiteLLMProviderChatStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		// Emit 3 chunks + final usage chunk + [DONE].
		writeChunk := func(payload string) {
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
		writeChunk(`{"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}`)
		writeChunk(`{"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":" there"}}]}`)
		writeChunk(`{"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	p := NewLiteLLMProvider(LiteLLMConfig{BaseURL: upstream.URL})
	chunkCh, errCh := p.ChatStream(context.Background(), ChatRequest{
		Model:    "m",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		Stream:   true,
	})

	var chunks []ChatStreamChunk
	for c := range chunkCh {
		chunks = append(chunks, c)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected stream error: %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[len(chunks)-1].Usage == nil || chunks[len(chunks)-1].Usage.TotalTokens != 6 {
		t.Errorf("expected final usage on last chunk, got %+v", chunks[len(chunks)-1].Usage)
	}
}

func TestLiteLLMProviderChatStreamUpstreamFail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer upstream.Close()

	p := NewLiteLLMProvider(LiteLLMConfig{BaseURL: upstream.URL})
	chunkCh, errCh := p.ChatStream(context.Background(), ChatRequest{
		Model: "m", Messages: []ChatMessage{{Role: "user", Content: "x"}},
		Stream: true,
	})
	// Drain chunks (should be none).
	for range chunkCh {
	}
	err := <-errCh
	if err == nil {
		t.Fatal("expected stream error")
	}
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != ErrCodeUpstreamError {
		t.Errorf("expected UPSTREAM_ERROR, got %q", apiErr.Code)
	}
}

func TestLiteLLMProviderEmbeddingsHappyPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object":"list","model":"m",
			"data":[{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]}],
			"usage":{"prompt_tokens":3,"total_tokens":3}
		}`))
	}))
	defer upstream.Close()

	p := NewLiteLLMProvider(LiteLLMConfig{BaseURL: upstream.URL})
	resp, err := p.Embeddings(context.Background(), EmbeddingsRequest{Model: "m", Input: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Errorf("expected 1 embedding, got %d", len(resp.Data))
	}
}

func TestLiteLLMProviderImagesHappyPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"https://x"}]}`))
	}))
	defer upstream.Close()

	p := NewLiteLLMProvider(LiteLLMConfig{BaseURL: upstream.URL})
	resp, err := p.Images(context.Background(), ImagesRequest{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Data[0].URL != "https://x" {
		t.Errorf("expected URL passthrough, got %q", resp.Data[0].URL)
	}
}

func TestProviderNameUsedAsCostLabel(t *testing.T) {
	p := NewLiteLLMProvider(LiteLLMConfig{ProviderID: "litellm", BaseURL: "http://x"})
	if p.Name() != "litellm" {
		t.Errorf("expected litellm, got %q", p.Name())
	}
}

func TestProviderNameDefaultsToLiteLLM(t *testing.T) {
	p := NewLiteLLMProvider(LiteLLMConfig{BaseURL: "http://x"})
	if p.Name() != "litellm" {
		t.Errorf("expected default name 'litellm', got %q", p.Name())
	}
}

// ─── models + capability table ────────────────────────────────────────────

func TestListModelsPullsFromCatalog(t *testing.T) {
	out := ListModels()
	if len(out.Models) == 0 {
		t.Fatal("expected non-empty model list")
	}
	for _, m := range out.Models {
		if m.ID == "" || m.Provider == "" {
			t.Errorf("expected populated id+provider, got %+v", m)
		}
	}
}

func TestModelsIncludeKnownIDs(t *testing.T) {
	out := ListModels()
	want := []string{
		"openrouter/qwen/qwen-2.5-7b-instruct",
		"anthropic/claude-haiku-4-5-20251001",
		"openai/gpt-4o-mini",
	}
	have := make(map[string]bool, len(out.Models))
	for _, m := range out.Models {
		have[m.ID] = true
	}
	for _, id := range want {
		if !have[id] {
			t.Errorf("expected model %q in list", id)
		}
	}
}

func TestModelsCapabilityFlags(t *testing.T) {
	out := ListModels()
	for _, m := range out.Models {
		// Every catalog entry should at least support streaming
		// (current default for OpenAI-compat upstreams).
		if !m.SupportsStreaming {
			t.Errorf("expected streaming=true for %q", m.ID)
		}
	}
}

// ─── SSE writer ──────────────────────────────────────────────────────────

func TestSSEWriterEmitsChunksAndDone(t *testing.T) {
	rec := httptest.NewRecorder()
	sw, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := sw.WriteChunk(ChatStreamChunk{
		ID: "c", Object: "chat.completion.chunk", Created: 1, Model: "m",
		Choices: []ChatStreamChoice{{Index: 0, Delta: ChatMessage{Content: "hi"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sw.End(); err != nil {
		t.Fatal(err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `data: {"id":"c"`) {
		t.Errorf("expected data: prefix and id, got %q", body)
	}
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Errorf("expected [DONE] terminator, got tail=%q", body[len(body)-32:])
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream; charset=utf-8" {
		t.Errorf("expected SSE content-type, got %q", ct)
	}
}

func TestSSEWriterError(t *testing.T) {
	rec := httptest.NewRecorder()
	sw, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	sw.WriteError("UPSTREAM_ERROR", "boom")
	body := rec.Body.String()
	if !strings.Contains(body, "UPSTREAM_ERROR") {
		t.Errorf("expected error code in body, got %q", body)
	}
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Errorf("expected [DONE] terminator, got %q", body)
	}
}

// ─── Misc ────────────────────────────────────────────────────────────────

func TestAPIErrorHTTPStatusDefaults(t *testing.T) {
	cases := map[string]int{
		ErrCodeInvalidRequest:    http.StatusBadRequest,
		ErrCodeModelRateLimited:  http.StatusTooManyRequests,
		ErrCodeModelNotSupported: http.StatusBadRequest,
		ErrCodeBudgetExceeded:    http.StatusPaymentRequired,
		ErrCodeUpstreamError:     http.StatusBadGateway,
	}
	for code, want := range cases {
		e := &APIError{Code: code, Message: "x"}
		if got := e.HTTPStatus(); got != want {
			t.Errorf("HTTPStatus(%q) = %d, want %d", code, got, want)
		}
	}
}

func TestReadBodyLimited(t *testing.T) {
	src := bytes.NewReader(make([]byte, 10))
	got, err := readBodyLimited(src, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("expected 5 bytes, got %d", len(got))
	}
}

// Make sure Now is overridable.
func TestNowIsOverridable(t *testing.T) {
	orig := Now
	defer func() { Now = orig }()
	Now = func() int64 { return 42 }
	if Now() != 42 {
		t.Errorf("Now override failed")
	}
}

// ─── ensure error.message extraction tolerates non-JSON ───────────────────

func TestExtractUpstreamMessageEmpty(t *testing.T) {
	if got := extractUpstreamMessage(nil); got != "" {
		t.Errorf("expected empty on nil body, got %q", got)
	}
	if got := extractUpstreamMessage([]byte("not json")); got != "" {
		t.Errorf("expected empty on non-JSON, got %q", got)
	}
}

func TestPreviewBodyTruncates(t *testing.T) {
	big := strings.Repeat("a", 600)
	out := previewBody([]byte(big))
	if !strings.HasSuffix(out, "…") {
		t.Errorf("expected truncation marker, got tail=%q", out[len(out)-10:])
	}
	short := previewBody([]byte("ok"))
	if short != "ok" {
		t.Errorf("expected short body untouched, got %q", short)
	}
}

// ─── response_cost extraction ────────────────────────────────────────────

func TestExtractResponseCostPresent(t *testing.T) {
	cost, ok := extractResponseCost([]byte(`{"response_cost":0.012,"id":"x"}`))
	if !ok {
		t.Fatalf("expected response_cost to be extracted")
	}
	if cost != 0.012 {
		t.Errorf("expected 0.012, got %f", cost)
	}
}

func TestExtractResponseCostAbsent(t *testing.T) {
	if _, ok := extractResponseCost([]byte(`{"id":"x"}`)); ok {
		t.Errorf("expected ok=false when response_cost absent")
	}
}

func TestExtractResponseCostMalformed(t *testing.T) {
	if _, ok := extractResponseCost([]byte("not json")); ok {
		t.Errorf("expected ok=false on non-JSON body")
	}
	if _, ok := extractResponseCost(nil); ok {
		t.Errorf("expected ok=false on nil body")
	}
}

// Verify provider.do uses configured timeout (not strictly testing the
// timeout firing, just that the construction is correct).
func TestProviderHasTimeout(t *testing.T) {
	p := NewLiteLLMProvider(LiteLLMConfig{
		BaseURL: "http://example", Timeout: 5 * time.Second,
	})
	if p.client.Timeout != 5*time.Second {
		t.Errorf("expected 5s timeout, got %v", p.client.Timeout)
	}
}

// chatRequestBody forces stream to the wrapper value, not the embedded.
func TestChatRequestBodyOverridesStreamFlag(t *testing.T) {
	out := chatRequestBody(ChatRequest{
		Model: "m", Messages: []ChatMessage{{Role: "user", Content: "x"}}, Stream: true,
	}, false)
	var aux map[string]any
	if err := json.Unmarshal(out, &aux); err != nil {
		t.Fatal(err)
	}
	if aux["stream"] != false {
		t.Errorf("expected stream=false in body, got %v", aux["stream"])
	}
}

// Ensure validation errors surface cleanly from streaming entry too.
func TestGatewayChatStreamValidationError(t *testing.T) {
	g := New(&fakeProvider{})
	chunkCh, errCh := g.ChatStream(context.Background(), ChatRequest{Model: "m"})
	for range chunkCh {
	}
	err := <-errCh
	apiErr, ok := AsAPIError(err)
	if !ok || apiErr.Code != ErrCodeInvalidRequest {
		t.Errorf("expected INVALID_REQUEST, got %v", err)
	}
}

// Ensure Gateway.ChatStream forwards chunks from the provider.
func TestGatewayChatStreamForwards(t *testing.T) {
	g := New(&fakeProvider{
		chunks: []ChatStreamChunk{
			{ID: "x", Object: "chat.completion.chunk", Created: 1, Model: "m",
				Choices: []ChatStreamChoice{{Index: 0, Delta: ChatMessage{Content: "a"}}}},
			{ID: "x", Object: "chat.completion.chunk", Created: 1, Model: "m",
				Choices: []ChatStreamChoice{{Index: 0, Delta: ChatMessage{Content: "b"}}}},
		},
	})
	chunkCh, errCh := g.ChatStream(context.Background(), ChatRequest{
		Model: "m", Messages: []ChatMessage{{Role: "user", Content: "x"}}, Stream: true,
	})
	var out []ChatStreamChunk
	for c := range chunkCh {
		out = append(out, c)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(out))
	}
}

// Verify ProviderName falls through cleanly when unset.
func TestProviderNameFallback(t *testing.T) {
	g := &Gateway{}
	if g.ProviderName() != "unset" {
		t.Errorf("expected 'unset' for empty gateway, got %q", g.ProviderName())
	}
}

// Verify reading a closed body returns nil/no error.
func TestReadBodyLimitedNil(t *testing.T) {
	got, err := readBodyLimited(nil, 0)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil body, got %v", got)
	}
}

// Verify the SSEWriter rejects a non-Flusher writer.
type noFlushWriter struct{ http.ResponseWriter }

func (n *noFlushWriter) Header() http.Header        { return http.Header{} }
func (n *noFlushWriter) Write(b []byte) (int, error) { return len(b), nil }
func (n *noFlushWriter) WriteHeader(int)             {}

func TestSSEWriterRequiresFlusher(t *testing.T) {
	_, err := NewSSEWriter(&noFlushWriter{})
	if err == nil {
		t.Error("expected error for non-Flusher writer")
	}
}

// Verify SSEWriter raw write works.
func TestSSEWriterRaw(t *testing.T) {
	rec := httptest.NewRecorder()
	sw, _ := NewSSEWriter(rec)
	if err := sw.WriteRaw([]byte(`{"x":1}`)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `data: {"x":1}`) {
		t.Errorf("expected raw data, got %q", rec.Body.String())
	}
}

// Verify failed writer state stops further writes.
type failWriter struct{ httptest.ResponseRecorder }

func (f *failWriter) Write(b []byte) (int, error) { return 0, io.ErrClosedPipe }
func (f *failWriter) Flush()                       {}

func TestSSEWriterFailedStops(t *testing.T) {
	rec := &failWriter{}
	rec.HeaderMap = make(http.Header)
	sw, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	// First write should fail.
	if err := sw.WriteChunk(ChatStreamChunk{ID: "x"}); err == nil {
		t.Fatal("expected write error")
	}
	if sw.Failed() == nil {
		t.Error("expected Failed() to return error")
	}
	// Subsequent writes should short-circuit.
	if err := sw.WriteChunk(ChatStreamChunk{ID: "y"}); err == nil {
		t.Error("expected short-circuit error")
	}
}