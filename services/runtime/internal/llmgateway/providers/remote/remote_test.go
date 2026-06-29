// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/adapters/remote"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
)

type mock struct {
	caps       capsEnv
	chatResp   string
	chatStatus int
	streamSSE  []string
	embedResp  string
	models     string
}

type capsEnv struct {
	Name            string         `json:"name"`
	Version         string         `json:"version"`
	Slot            string         `json:"slot"`
	ProtocolVersion string         `json:"protocol_version"`
	Capabilities    map[string]any `json:"capabilities"`
}

func defaultCaps() capsEnv {
	return capsEnv{
		Name:            "test-llm",
		Version:         "1.0.0",
		Slot:            "llm-chat",
		ProtocolVersion: "v1",
		Capabilities: map[string]any{
			"supports_chat":       true,
			"supports_embeddings": true,
			"supports_streaming":  true,
			"supports_tools":      true,
			"model_prefixes":      []string{"test/"},
		},
	}
}

func newMock(t *testing.T, m *mock) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(m.caps)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req llmgateway.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			for _, line := range m.streamSSE {
				_, _ = fmt.Fprintln(w, "data: "+line)
				_, _ = fmt.Fprintln(w)
				fl.Flush()
			}
			return
		}
		if m.chatStatus == 0 {
			m.chatStatus = http.StatusOK
		}
		w.WriteHeader(m.chatStatus)
		if m.chatResp == "" {
			m.chatResp = `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		}
		_, _ = io.WriteString(w, m.chatResp)
	})
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		if m.embedResp == "" {
			m.embedResp = `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]}],"model":"x","usage":{"prompt_tokens":1,"total_tokens":1}}`
		}
		_, _ = io.WriteString(w, m.embedResp)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		if m.models == "" {
			m.models = `{"object":"list","data":[{"id":"test/foo","object":"model","verbs":["chat"]}]}`
		}
		_, _ = io.WriteString(w, m.models)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newAdapter(t *testing.T, m *mock) *Adapter {
	t.Helper()
	if m == nil {
		m = &mock{}
	}
	if m.caps.Name == "" {
		m.caps = defaultCaps()
	}
	srv := newMock(t, m)
	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAdapter_CapabilitiesParsed(t *testing.T) {
	a := newAdapter(t, nil)
	if a.Name() != "test-llm" {
		t.Errorf("name: %s", a.Name())
	}
	c := a.ProviderCaps()
	if !c.SupportsChat || !c.SupportsEmbeddings || !c.SupportsTools {
		t.Errorf("caps wrong: %+v", c)
	}
	if len(c.ModelPrefixes) != 1 || c.ModelPrefixes[0] != "test/" {
		t.Errorf("prefixes: %+v", c.ModelPrefixes)
	}
}

func TestAdapter_Chat(t *testing.T) {
	a := newAdapter(t, nil)
	resp, err := a.Chat(context.Background(), llmgateway.ChatRequest{
		Model:    "test/foo",
		Messages: []llmgateway.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("no choices")
	}
	if resp.Usage == nil || resp.Usage.TotalTokens == 0 {
		t.Errorf("missing usage: %+v", resp.Usage)
	}
}

func TestAdapter_ChatErrorMapping(t *testing.T) {
	m := &mock{
		caps:       defaultCaps(),
		chatStatus: http.StatusNotFound,
		chatResp:   `{"code":"model_not_found","status":404,"title":"Model not found"}`,
	}
	srv := newMock(t, m)
	a, err := New(context.Background(), Config{BaseURL: srv.URL, MaxRetries: 0})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Chat(context.Background(), llmgateway.ChatRequest{Model: "test/nope"})
	if !remote.IsCode(err, "model_not_found") {
		t.Errorf("expected model_not_found, got %v", err)
	}
}

func TestAdapter_ChatStream(t *testing.T) {
	m := &mock{
		caps: defaultCaps(),
		streamSSE: []string{
			`{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hel"}}]}`,
			`{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
			`{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`[DONE]`,
		},
	}
	srv := newMock(t, m)
	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	chunks, errs := a.ChatStream(context.Background(), llmgateway.ChatRequest{
		Model:    "test/foo",
		Messages: []llmgateway.ChatMessage{{Role: "user", Content: "hi"}},
	})
	var got strings.Builder
	for ch := range chunks {
		for _, c := range ch.Choices {
			if s, ok := c.Delta.Content.(string); ok {
				got.WriteString(s)
			}
		}
	}
	if err := <-errs; err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if got.String() != "hello" {
		t.Errorf("got %q want hello", got.String())
	}
}

func TestAdapter_ChatStreamNoDONE(t *testing.T) {
	m := &mock{
		caps:      defaultCaps(),
		streamSSE: []string{`{"id":"x","object":"chunk","choices":[{"index":0,"delta":{"content":"x"}}]}`},
	}
	srv := newMock(t, m)
	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	chunks, errs := a.ChatStream(context.Background(), llmgateway.ChatRequest{Model: "test/foo"})
	// MUST drain chunks for the shim's goroutine to make progress.
	go func() {
		for range chunks {
		}
	}()
	err = <-errs
	if err == nil || !strings.Contains(err.Error(), "[DONE]") {
		t.Errorf("expected [DONE] error, got %v", err)
	}
}

func TestAdapter_Embeddings(t *testing.T) {
	a := newAdapter(t, nil)
	resp, err := a.Embeddings(context.Background(), llmgateway.EmbeddingsRequest{
		Model: "test/embed",
		Input: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 {
		t.Errorf("data length: %d", len(resp.Data))
	}
}

func TestAdapter_ListModels(t *testing.T) {
	a := newAdapter(t, nil)
	cat, err := a.ListModels(context.Background(), "chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Data) != 1 || cat.Data[0].ID != "test/foo" {
		t.Errorf("models: %+v", cat.Data)
	}
}

func TestAdapter_RejectsWrongSlot(t *testing.T) {
	m := &mock{caps: defaultCaps()}
	m.caps.Slot = "billing"
	srv := newMock(t, m)
	_, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "slot") {
		t.Errorf("expected slot mismatch, got %v", err)
	}
}

func TestAdapter_Probe(t *testing.T) {
	a := newAdapter(t, nil)
	st, err := a.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st != "healthy" {
		t.Errorf("status: %s", st)
	}
}

func TestAdapter_StreamCancellable(t *testing.T) {
	// Slow-server scenario: many chunks; cancel mid-stream.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(defaultCaps())
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for i := 0; i < 100; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"%d \"}}]}\n\n", i)
			fl.Flush()
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chunks, _ := a.ChatStream(ctx, llmgateway.ChatRequest{Model: "test/x"})
	count := 0
	for range chunks {
		count++
		if count >= 3 {
			cancel()
		}
	}
	if count == 0 {
		t.Error("got 0 chunks before cancel")
	}
}

func TestResponseCostFromHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("X-Backai-Response-Cost-Usd", "0.0012")
	h.Set("X-Backai-Model-Used", "test/gpt")
	cost, model, ok := ResponseCostFromHeaders(h)
	if !ok || cost != 0.0012 || model != "test/gpt" {
		t.Errorf("got cost=%v model=%q ok=%v", cost, model, ok)
	}
	if _, _, ok := ResponseCostFromHeaders(http.Header{}); ok {
		t.Error("expected ok=false for empty headers")
	}
}
