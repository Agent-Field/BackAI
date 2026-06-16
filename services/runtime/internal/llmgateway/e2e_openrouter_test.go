// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package llmgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestE2E_OpenRouter_Kimi verifies our HTTP layer can drive a real
// OpenAI-compatible chat completion against OpenRouter using Kimi
// (moonshotai/kimi-k2.5). This exercises the same wire shape the
// runtime's gateway uses and proves connectivity + auth + decoding.
//
// Skips silently when OPENROUTER_API_KEY is unset.
//
// Build tag: e2e. Run with:
//
//	OPENROUTER_API_KEY=... go test -tags=e2e ./services/runtime/internal/llmgateway/ -run TestE2E_OpenRouter_Kimi -v
func TestE2E_OpenRouter_Kimi(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		t.Skip("OPENROUTER_API_KEY not set; skipping real LLM E2E")
	}

	// Non-thinking Kimi variant — emits content tokens, not pure reasoning.
	body := map[string]any{
		"model": "moonshotai/kimi-k2-0905",
		"messages": []map[string]string{
			{"role": "system", "content": "Reply with ONLY the literal word OK and nothing else."},
			{"role": "user", "content": "Are we connected?"},
		},
		"max_tokens":  16,
		"temperature": 0.0,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/Agent-Field/backai")
	req.Header.Set("X-Title", "BackAI E2E Test")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		t.Fatalf("upstream %d: %s", resp.StatusCode, string(buf))
	}

	var out struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if out.ID == "" {
		t.Error("response missing id")
	}
	if !strings.HasPrefix(out.Model, "moonshotai/kimi") {
		t.Errorf("expected kimi model in response, got %q", out.Model)
	}
	if len(out.Choices) == 0 {
		t.Fatal("no choices returned")
	}
	if out.Choices[0].Message.Content == "" {
		t.Error("empty assistant content")
	}
	if out.Usage.TotalTokens == 0 {
		t.Error("usage.total_tokens=0; upstream not reporting usage")
	}

	t.Logf("real LLM E2E: model=%s tokens=%d content=%q",
		out.Model, out.Usage.TotalTokens,
		strings.TrimSpace(out.Choices[0].Message.Content))
}
