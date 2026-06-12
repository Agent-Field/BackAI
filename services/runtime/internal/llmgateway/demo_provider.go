// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DemoProvider is a deterministic, no-key provider for the public first-run
// SupportDesk experience. It never calls an upstream model. The provider name
// is written into cost events as "demo" so operators can tell demo traffic from
// real provider traffic.
type DemoProvider struct{}

// NewDemoProvider constructs the deterministic no-key provider.
func NewDemoProvider() *DemoProvider {
	return &DemoProvider{}
}

func (p *DemoProvider) Name() string { return "demo" }

func (p *DemoProvider) Chat(_ context.Context, req ChatRequest) (ChatResponse, error) {
	now := time.Now().UTC()
	content := demoSupportDeskReply(req)
	usage := demoUsage(req, content)
	cost := demoCostUSD(usage)
	usage.ResponseCostUSD = &cost

	return ChatResponse{
		ID:      "chatcmpl-demo-" + now.Format("20060102T150405.000000000"),
		Object:  "chat.completion",
		Created: now.Unix(),
		Model:   req.Model,
		Choices: []ChatChoice{{
			Index: 0,
			Message: ChatMessage{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: "stop",
		}},
		Usage:             usage,
		SystemFingerprint: "demo-supportdesk",
	}, nil
}

func (p *DemoProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatStreamChunk, <-chan error) {
	chunkCh := make(chan ChatStreamChunk, 16)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		now := time.Now().UTC()
		id := "chatcmpl-demo-" + now.Format("20060102T150405.000000000")
		content := demoSupportDeskReply(req)
		parts := splitDemoStream(content)

		for _, part := range parts {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case chunkCh <- ChatStreamChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: now.Unix(),
				Model:   req.Model,
				Choices: []ChatStreamChoice{{
					Index: 0,
					Delta: ChatMessage{Role: "assistant", Content: part},
				}},
				SystemFingerprint: "demo-supportdesk",
			}:
			}
		}

		usage := demoUsage(req, content)
		cost := demoCostUSD(usage)
		usage.ResponseCostUSD = &cost
		select {
		case <-ctx.Done():
			errCh <- ctx.Err()
		case chunkCh <- ChatStreamChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: now.Unix(),
			Model:   req.Model,
			Choices: []ChatStreamChoice{{
				Index:        0,
				Delta:        ChatMessage{},
				FinishReason: "stop",
			}},
			Usage:             usage,
			SystemFingerprint: "demo-supportdesk",
		}:
		}
	}()

	return chunkCh, errCh
}

func (p *DemoProvider) Embeddings(context.Context, EmbeddingsRequest) (EmbeddingsResponse, error) {
	return EmbeddingsResponse{}, demoUnsupported("embeddings")
}

func (p *DemoProvider) Images(context.Context, ImagesRequest) (ImagesResponse, error) {
	return ImagesResponse{}, demoUnsupported("images")
}

func (p *DemoProvider) AudioSpeech(context.Context, AudioSpeechRequest) (RawResponse, error) {
	return RawResponse{}, demoUnsupported("audio speech")
}

func (p *DemoProvider) AudioTranscription(context.Context, AudioTranscriptionRequest) (RawResponse, error) {
	return RawResponse{}, demoUnsupported("audio transcription")
}

func demoUnsupported(op string) error {
	return &APIError{
		Code:    ErrCodeModelNotSupported,
		Message: fmt.Sprintf("demo provider supports chat completions only; configure a real provider for %s", op),
		Status:  http.StatusServiceUnavailable,
	}
}

func demoSupportDeskReply(req ChatRequest) string {
	user := strings.TrimSpace(lastUserText(req.Messages))
	if user == "" {
		user = "The customer needs help with their support request."
	}
	ticket := demoCustomerTicket(user)
	return fmt.Sprintf(`Hi there,

Thanks for flagging this. I understand the concern and I can help investigate it.

I will review the account and invoice details, verify whether the charge was duplicated, and confirm the refund policy before making any changes. If the charge is incorrect, we will correct it and follow up with the next step.

For now, I am routing this to billing review so we do not promise a refund before the invoice and account are verified.

Customer issue: %s`, ticket)
}

func demoCustomerTicket(user string) string {
	ticket := strings.TrimSpace(user)
	if after, ok := strings.CutPrefix(ticket, "Customer ticket:"); ok {
		ticket = strings.TrimSpace(after)
	}
	if before, _, ok := strings.Cut(ticket, "\nAgentField support plan:"); ok {
		ticket = strings.TrimSpace(before)
	}
	if ticket == "" {
		return "The customer needs help with their support request."
	}
	return ticket
}

func lastUserText(messages []ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		switch v := messages[i].Content.(type) {
		case string:
			return v
		case []any:
			parts := make([]string, 0, len(v))
			for _, part := range v {
				m, ok := part.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
			return strings.Join(parts, " ")
		}
	}
	return ""
}

func demoUsage(req ChatRequest, content string) *Usage {
	promptChars := 0
	for _, msg := range req.Messages {
		promptChars += len(fmt.Sprint(msg.Content))
	}
	promptTokens := roughTokens(promptChars)
	completionTokens := roughTokens(len(content))
	return &Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
}

func roughTokens(chars int) int {
	if chars <= 0 {
		return 1
	}
	tokens := chars / 4
	if chars%4 != 0 {
		tokens++
	}
	if tokens < 1 {
		return 1
	}
	return tokens
}

func demoCostUSD(usage *Usage) float64 {
	if usage == nil {
		return 0.0001
	}
	// Tiny but non-zero, so the cost dashboard lights up in demo mode.
	return float64(usage.TotalTokens) * 0.0000005
}

func splitDemoStream(content string) []string {
	words := strings.Fields(content)
	if len(words) == 0 {
		return []string{content}
	}
	parts := make([]string, 0, len(words))
	for i, word := range words {
		if i == 0 {
			parts = append(parts, word)
			continue
		}
		parts = append(parts, " "+word)
	}
	return parts
}
