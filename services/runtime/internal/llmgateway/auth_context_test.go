// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLiteLLMAuthContext_Fallback verifies the backward-compat path:
// when no per-tenant LiteLLM key is on the context the provider uses
// the master key. This is the only behaviour pre-#22 builds had, and
// it MUST keep working for legacy suite_api_keys rows that never went
// through LiteLLM mirroring.
func TestLiteLLMAuthContext_Fallback(t *testing.T) {
	var seenAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[]}`))
	}))
	defer server.Close()

	p := NewLiteLLMProvider(LiteLLMConfig{
		BaseURL:   server.URL,
		MasterKey: "sk-master-fallback",
	})

	// No WithLiteLLMKey on ctx — provider should fall back to master.
	_, err := p.Chat(context.Background(), ChatRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if seenAuth != "Bearer sk-master-fallback" {
		t.Errorf("Authorization = %q, want master fallback", seenAuth)
	}
}

// TestLiteLLMAuthContext_PerTenantOverride verifies that when a
// per-tenant LiteLLM key is on the context the provider uses it
// instead of the master key.
func TestLiteLLMAuthContext_PerTenantOverride(t *testing.T) {
	var seenAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[]}`))
	}))
	defer server.Close()

	p := NewLiteLLMProvider(LiteLLMConfig{
		BaseURL:   server.URL,
		MasterKey: "sk-master",
	})

	ctx := WithLiteLLMKey(context.Background(), "sk-litellm-tenant-key")
	_, err := p.Chat(ctx, ChatRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if seenAuth != "Bearer sk-litellm-tenant-key" {
		t.Errorf("Authorization = %q, want per-tenant override", seenAuth)
	}
}

// TestLiteLLMAuthContext_EmptyKeyDoesntClobber verifies that an empty
// key on the context doesn't clobber the master-key fallback.
func TestLiteLLMAuthContext_EmptyKeyDoesntClobber(t *testing.T) {
	ctx := WithLiteLLMKey(context.Background(), "")
	if got := litellmKeyFromContext(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestLiteLLMAuthContext_NilCtx(t *testing.T) {
	if got := litellmKeyFromContext(nil); got != "" {
		t.Errorf("nil ctx should give empty, got %q", got)
	}
	// WithLiteLLMKey on a nil ctx should not panic.
	ctx := WithLiteLLMKey(nil, "sk")
	if got := litellmKeyFromContext(ctx); got != "sk" {
		t.Errorf("got %q", got)
	}
}
