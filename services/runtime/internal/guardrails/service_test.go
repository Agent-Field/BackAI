// SPDX-License-Identifier: Apache-2.0

package guardrails

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
)

func TestRegexRedactsPII(t *testing.T) {
	svc, err := New(Config{Enabled: true, RedactPII: true, Moderate: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.ProcessText(context.Background(), "email me at alice@example.com with card 4242 4242 4242 4242")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Text, "alice@example.com") || strings.Contains(res.Text, "4242 4242") {
		t.Fatalf("expected PII redacted, got %q", res.Text)
	}
	if !strings.Contains(res.Text, "[REDACTED_EMAIL_ADDRESS]") {
		t.Fatalf("expected email marker, got %q", res.Text)
	}
	if !strings.Contains(res.Text, "[REDACTED_CREDIT_CARD]") {
		t.Fatalf("expected card marker, got %q", res.Text)
	}
}

func TestModerationBlocksConfiguredPattern(t *testing.T) {
	svc, err := New(Config{
		Enabled:       true,
		RedactPII:     true,
		Moderate:      true,
		BlockPatterns: []string{`(?i)forbidden phrase`},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ProcessText(context.Background(), "this has a forbidden phrase")
	if err == nil {
		t.Fatal("expected moderation error")
	}
	if !strings.Contains(err.Error(), "forbidden phrase") {
		t.Fatalf("expected rule in error, got %v", err)
	}
}

func TestChatRequestRedactsTextPartsAndToolArgs(t *testing.T) {
	svc, err := New(Config{Enabled: true, RedactPII: true, Moderate: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := llmgateway.ChatRequest{
		Model: "m",
		Messages: []llmgateway.ChatMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": "alice@example.com"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/a.png"}},
				},
			},
			{
				Role: "assistant",
				ToolCalls: []llmgateway.ToolCall{{
					Type: "function",
					Function: llmgateway.ToolCallFunction{
						Name:      "lookup",
						Arguments: `{"email":"bob@example.com"}`,
					},
				}},
			},
		},
	}
	out, changed, err := svc.ProcessChatRequest(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected request changed")
	}
	encoded, _ := json.Marshal(out)
	got := string(encoded)
	if strings.Contains(got, "alice@example.com") || strings.Contains(got, "bob@example.com") {
		t.Fatalf("expected redacted request, got %s", got)
	}
	if !strings.Contains(got, "https://example.com/a.png") {
		t.Fatalf("expected image URL preserved, got %s", got)
	}
}

func TestPresidioAdapterRedactsViaAnalyzerAndAnonymizer(t *testing.T) {
	var analyzeCalled, anonymizeCalled bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/analyze":
			analyzeCalled = true
			writeTestJSON(t, w, []map[string]any{{
				"entity_type": "EMAIL_ADDRESS",
				"start":       11,
				"end":         28,
				"score":       0.99,
			}})
		case "/anonymize":
			anonymizeCalled = true
			writeTestJSON(t, w, map[string]any{"text": "contact [REDACTED_PII]"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer upstream.Close()

	svc, err := New(Config{
		Enabled:             true,
		RedactPII:           true,
		Provider:            ProviderPresidio,
		PresidioAnalyzerURL: upstream.URL,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.ProcessText(context.Background(), "contact me@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !analyzeCalled || !anonymizeCalled {
		t.Fatalf("expected both presidio endpoints called, analyze=%v anonymize=%v", analyzeCalled, anonymizeCalled)
	}
	if res.Text != "contact [REDACTED_PII]" {
		t.Fatalf("unexpected redacted text %q", res.Text)
	}
	if len(res.Findings) != 1 || res.Findings[0].Source != ProviderPresidio {
		t.Fatalf("unexpected findings %+v", res.Findings)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
