// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"context"
	"strings"
	"testing"
)

func TestDemoProviderChatReturnsSupportDeskReplyAndUsage(t *testing.T) {
	provider := NewDemoProvider()
	resp, err := provider.Chat(context.Background(), ChatRequest{
		Model: "demo/supportdesk",
		Messages: []ChatMessage{{
			Role:    "user",
			Content: "A customer says their invoice is wrong.",
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if provider.Name() != "demo" {
		t.Fatalf("provider name = %q, want demo", provider.Name())
	}
	got, _ := resp.Choices[0].Message.Content.(string)
	if !strings.Contains(got, "Draft reply") {
		t.Fatalf("reply missing support draft:\n%s", got)
	}
	if !strings.Contains(got, "invoice is wrong") {
		t.Fatalf("reply missing user context:\n%s", got)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens == 0 {
		t.Fatalf("usage not populated: %#v", resp.Usage)
	}
	if resp.Usage.ResponseCostUSD == nil || *resp.Usage.ResponseCostUSD <= 0 {
		t.Fatalf("response cost not populated: %#v", resp.Usage)
	}
}

func TestDemoProviderChatStreamEmitsContentAndFinalUsage(t *testing.T) {
	provider := NewDemoProvider()
	chunks, errs := provider.ChatStream(context.Background(), ChatRequest{
		Model: "demo/supportdesk",
		Messages: []ChatMessage{{
			Role:    "user",
			Content: "A customer wants a refund.",
		}},
	})

	var content strings.Builder
	var usage *Usage
	for chunk := range chunks {
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			if text, ok := choice.Delta.Content.(string); ok {
				content.WriteString(text)
			}
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
	}
	if !strings.Contains(content.String(), "refund") {
		t.Fatalf("stream content missing context:\n%s", content.String())
	}
	if usage == nil || usage.ResponseCostUSD == nil || *usage.ResponseCostUSD <= 0 {
		t.Fatalf("final usage/cost missing: %#v", usage)
	}
}
