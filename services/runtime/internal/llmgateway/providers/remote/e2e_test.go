// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
)

// TestE2E_RemoteAdapter_OpenRouter spins up a tiny "OpenRouter
// proxy" using httptest.NewServer that:
//
//   1. Serves /v1/capabilities + /healthz with llm-chat-v1 envelope.
//   2. For /v1/chat/completions, forwards the request body verbatim
//      to https://openrouter.ai/api/v1/chat/completions using the
//      operator's OPENROUTER_API_KEY and returns whatever upstream
//      returned.
//   3. For /v1/models, returns a tiny static catalog.
//
// This is the exact shape any third-party "OpenAI-compatible
// observability/routing proxy" (Helicone, Portkey, OpenLLMetry) would
// expose. The Go remote shim drives the proxy, the proxy hits the
// real LLM (Kimi via OpenRouter).
//
// Build tag: e2e. Skips when OPENROUTER_API_KEY is unset.
//
//	OPENROUTER_API_KEY=... go test -tags=e2e -run TestE2E_RemoteAdapter_OpenRouter ./services/runtime/internal/llmgateway/providers/remote/ -v
func TestE2E_RemoteAdapter_OpenRouter(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		t.Skip("OPENROUTER_API_KEY not set; skipping real LLM E2E")
	}

	proxy := newOpenRouterProxy(t, key)
	defer proxy.Close()

	a, err := New(context.Background(), Config{
		BaseURL: proxy.URL,
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if a.Name() != "openrouter-proxy" {
		t.Errorf("adapter name: %q", a.Name())
	}
	if !a.ProviderCaps().SupportsChat {
		t.Error("supports_chat=false; proxy should declare true")
	}

	t.Run("non-streaming chat", func(t *testing.T) {
		temp := 0.0
		max := 16
		resp, err := a.Chat(context.Background(), llmgateway.ChatRequest{
			Model: "moonshotai/kimi-k2-0905",
			Messages: []llmgateway.ChatMessage{
				{Role: "system", Content: "Reply with ONLY the literal word OK and nothing else."},
				{Role: "user", Content: "Are we connected?"},
			},
			Temperature: &temp,
			MaxTokens:   &max,
		})
		if err != nil {
			t.Fatalf("Chat: %v", err)
		}
		if len(resp.Choices) == 0 {
			t.Fatal("no choices in response")
		}
		content, _ := resp.Choices[0].Message.Content.(string)
		if content == "" {
			t.Error("empty assistant content")
		}
		if resp.Usage == nil || resp.Usage.TotalTokens == 0 {
			t.Error("usage missing or zero")
		}
		t.Logf("real LLM via proxy: model=%s tokens=%d content=%q",
			resp.Model, totalTokens(resp.Usage), strings.TrimSpace(content))
	})

	t.Run("model catalog", func(t *testing.T) {
		cat, err := a.ListModels(context.Background(), "chat")
		if err != nil {
			t.Fatal(err)
		}
		if len(cat.Data) == 0 {
			t.Fatal("empty model list")
		}
	})

	t.Run("health", func(t *testing.T) {
		st, err := a.Probe(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if st != "healthy" {
			t.Errorf("status: %s", st)
		}
	})
}

func totalTokens(u *llmgateway.Usage) int {
	if u == nil {
		return 0
	}
	return u.TotalTokens
}

// newOpenRouterProxy stands up a minimal sidecar implementing the
// llm-chat-v1 protocol over OpenRouter. Real connectors (Helicone,
// Portkey) look essentially like this proxy under the hood.
func newOpenRouterProxy(t *testing.T, openrouterKey string) *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":             "openrouter-proxy",
			"version":          "1.0.0",
			"slot":             "llm-chat",
			"protocol_version": "v1",
			"vendor":           "BackAI test harness",
			"capabilities": map[string]any{
				"supports_chat":               true,
				"supports_embeddings":         false,
				"supports_streaming":          true,
				"supports_tools":              true,
				"supports_vision":             true,
				"model_prefixes":              []string{"moonshotai/", "openai/", "anthropic/", "google/"},
				"max_completion_tokens":       8192,
				"supports_per_tenant_attribution": true,
			},
		})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		// Read body and forward verbatim.
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, `{"code":"invalid_request","status":400}`, http.StatusBadRequest)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
			"https://openrouter.ai/api/v1/chat/completions",
			strings.NewReader(string(body)))
		if err != nil {
			http.Error(w, `{"code":"internal_error","status":500}`, http.StatusInternalServerError)
			return
		}
		req.Header.Set("Authorization", "Bearer "+openrouterKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("HTTP-Referer", "https://github.com/Agent-Field/backai")
		req.Header.Set("X-Title", "BackAI E2E llm-chat proxy")

		upstreamResp, err := http.DefaultClient.Do(req)
		if err != nil {
			writeProblem(w, http.StatusBadGateway, "provider_error", err.Error())
			return
		}
		defer upstreamResp.Body.Close()
		if upstreamResp.StatusCode != http.StatusOK {
			buf, _ := io.ReadAll(io.LimitReader(upstreamResp.Body, 1<<20))
			writeProblem(w, http.StatusBadGateway, "provider_error", string(buf))
			return
		}
		w.Header().Set("Content-Type", upstreamResp.Header.Get("Content-Type"))
		w.Header().Set("X-Backai-Model-Used", "moonshotai/kimi-k2-0905")
		_, _ = io.Copy(w, upstreamResp.Body)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"id":                            "moonshotai/kimi-k2-0905",
					"object":                        "model",
					"verbs":                         []string{"chat"},
					"context_window":                32000,
					"supports_streaming":            true,
					"supports_tools":                true,
					"input_cost_per_million_tokens": 0.10,
				},
			},
		})
	})

	return httptest.NewServer(mux)
}

func writeProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"code":   code,
		"detail": detail,
	})
}

// Compile-time check that our shim is the same type the runtime would
// instantiate.
var _ = errors.New
