// SPDX-License-Identifier: Apache-2.0

package guardrails

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
)

type Service struct {
	cfg        Config
	log        *slog.Logger
	presidio   *presidioClient
	blockRules []compiledBlockRule
}

type compiledBlockRule struct {
	raw string
	re  *regexp.Regexp
}

func New(cfg Config, log *slog.Logger) (*Service, error) {
	if log == nil {
		log = slog.Default()
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.MaxTextBytes <= 0 {
		cfg.MaxTextBytes = 1 << 20
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}
	if strings.TrimSpace(cfg.Provider) == "" {
		cfg.Provider = ProviderRegex
	}
	cfg.Provider = strings.ToLower(strings.TrimSpace(cfg.Provider))

	rules := make([]compiledBlockRule, 0, len(cfg.BlockPatterns))
	for _, raw := range cfg.BlockPatterns {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		re, err := regexp.Compile(raw)
		if err != nil {
			return nil, fmt.Errorf("guardrails moderation pattern %q: %w", raw, err)
		}
		rules = append(rules, compiledBlockRule{raw: raw, re: re})
	}

	s := &Service{
		cfg:        cfg,
		log:        log,
		blockRules: rules,
	}
	if cfg.Provider == ProviderPresidio {
		s.presidio = newPresidioClient(cfg)
		if s.presidio == nil {
			return nil, fmt.Errorf("presidio provider requires AF_STACK_PRESIDIO_ANALYZER_URL")
		}
	}
	return s, nil
}

func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

func (s *Service) Provider() string {
	if s == nil {
		return ""
	}
	return s.cfg.Provider
}

func (s *Service) ProcessText(ctx context.Context, text string) (Result, error) {
	res := Result{Text: text}
	if !s.Enabled() || text == "" {
		return res, nil
	}
	if s.cfg.Moderate {
		for _, rule := range s.blockRules {
			if rule.re.MatchString(text) {
				res.Blocked = true
				res.Rule = rule.raw
				return res, &ModerationError{Rule: rule.raw}
			}
		}
	}
	if !s.cfg.RedactPII || len(text) > s.cfg.MaxTextBytes {
		return res, nil
	}

	switch s.cfg.Provider {
	case ProviderPresidio:
		redacted, findings, changed, err := s.presidio.redact(ctx, text)
		if err != nil {
			return res, &ProviderError{Provider: ProviderPresidio, Err: err}
		}
		res.Text = redacted
		res.Findings = findings
		res.Redacted = changed
		res.Provider = ProviderPresidio
		return res, nil
	case ProviderRegex:
		fallthrough
	default:
		redacted, findings, changed := redactWithRegex(text)
		res.Text = redacted
		res.Findings = findings
		res.Redacted = changed
		res.Provider = ProviderRegex
		return res, nil
	}
}

func (s *Service) ProcessChatRequest(ctx context.Context, req llmgateway.ChatRequest) (llmgateway.ChatRequest, bool, error) {
	changed := false
	for i := range req.Messages {
		msg, c, err := s.ProcessChatMessage(ctx, req.Messages[i])
		if err != nil {
			return req, changed, err
		}
		req.Messages[i] = msg
		changed = changed || c
	}
	if req.User != "" {
		res, err := s.ProcessText(ctx, req.User)
		if err != nil {
			return req, changed, err
		}
		if res.Text != req.User {
			req.User = res.Text
			changed = true
		}
	}
	return req, changed, nil
}

func (s *Service) ProcessChatMessage(ctx context.Context, msg llmgateway.ChatMessage) (llmgateway.ChatMessage, bool, error) {
	changed := false
	content, c, err := s.processAnyText(ctx, msg.Content)
	if err != nil {
		return msg, changed, err
	}
	msg.Content = content
	changed = changed || c
	for i := range msg.ToolCalls {
		args := msg.ToolCalls[i].Function.Arguments
		if args == "" {
			continue
		}
		res, err := s.ProcessText(ctx, args)
		if err != nil {
			return msg, changed, err
		}
		if res.Text != args {
			msg.ToolCalls[i].Function.Arguments = res.Text
			changed = true
		}
	}
	return msg, changed, nil
}

func (s *Service) ProcessChatResponse(ctx context.Context, resp llmgateway.ChatResponse) (llmgateway.ChatResponse, bool, error) {
	changed := false
	for i := range resp.Choices {
		msg, c, err := s.ProcessChatMessage(ctx, resp.Choices[i].Message)
		if err != nil {
			return resp, changed, err
		}
		resp.Choices[i].Message = msg
		changed = changed || c
	}
	return resp, changed, nil
}

func (s *Service) ProcessChatStreamChunk(ctx context.Context, chunk llmgateway.ChatStreamChunk) (llmgateway.ChatStreamChunk, bool, error) {
	changed := false
	for i := range chunk.Choices {
		msg, c, err := s.ProcessChatMessage(ctx, chunk.Choices[i].Delta)
		if err != nil {
			return chunk, changed, err
		}
		chunk.Choices[i].Delta = msg
		changed = changed || c
	}
	return chunk, changed, nil
}

func (s *Service) ProcessEmbeddingsRequest(ctx context.Context, req llmgateway.EmbeddingsRequest) (llmgateway.EmbeddingsRequest, bool, error) {
	input, changed, err := s.processAnyText(ctx, req.Input)
	if err != nil {
		return req, changed, err
	}
	req.Input = input
	if req.User != "" {
		res, err := s.ProcessText(ctx, req.User)
		if err != nil {
			return req, changed, err
		}
		if res.Text != req.User {
			req.User = res.Text
			changed = true
		}
	}
	return req, changed, nil
}

func (s *Service) ProcessImagesRequest(ctx context.Context, req llmgateway.ImagesRequest) (llmgateway.ImagesRequest, bool, error) {
	changed := false
	res, err := s.ProcessText(ctx, req.Prompt)
	if err != nil {
		return req, changed, err
	}
	if res.Text != req.Prompt {
		req.Prompt = res.Text
		changed = true
	}
	if req.User != "" {
		res, err = s.ProcessText(ctx, req.User)
		if err != nil {
			return req, changed, err
		}
		if res.Text != req.User {
			req.User = res.Text
			changed = true
		}
	}
	return req, changed, nil
}

func (s *Service) ProcessStringMap(ctx context.Context, raw map[string]any, fields ...string) (map[string]any, bool, error) {
	changed := false
	for _, field := range fields {
		value, ok := raw[field].(string)
		if !ok || value == "" {
			continue
		}
		res, err := s.ProcessText(ctx, value)
		if err != nil {
			return raw, changed, err
		}
		if res.Text != value {
			raw[field] = res.Text
			changed = true
		}
	}
	return raw, changed, nil
}

func (s *Service) ProcessResponseBytes(ctx context.Context, body []byte, contentType string) ([]byte, bool, error) {
	if !s.Enabled() || len(body) == 0 {
		return body, false, nil
	}
	if strings.Contains(strings.ToLower(contentType), "json") {
		var value any
		if err := json.Unmarshal(body, &value); err != nil {
			return body, false, nil
		}
		next, changed, err := s.processAnyText(ctx, value)
		if err != nil {
			return body, changed, err
		}
		if !changed {
			return body, false, nil
		}
		out, err := json.Marshal(next)
		if err != nil {
			return body, changed, err
		}
		return out, true, nil
	}
	if strings.HasPrefix(strings.ToLower(contentType), "text/") {
		res, err := s.ProcessText(ctx, string(body))
		if err != nil {
			return body, false, err
		}
		if res.Text != string(body) {
			return []byte(res.Text), true, nil
		}
	}
	return body, false, nil
}

func (s *Service) processAnyText(ctx context.Context, value any) (any, bool, error) {
	switch v := value.(type) {
	case string:
		res, err := s.ProcessText(ctx, v)
		if err != nil {
			return value, false, err
		}
		return res.Text, res.Text != v, nil
	case []any:
		changed := false
		out := make([]any, len(v))
		for i := range v {
			next, c, err := s.processAnyText(ctx, v[i])
			if err != nil {
				return value, changed, err
			}
			out[i] = next
			changed = changed || c
		}
		return out, changed, nil
	case []string:
		changed := false
		out := make([]string, len(v))
		for i := range v {
			res, err := s.ProcessText(ctx, v[i])
			if err != nil {
				return value, changed, err
			}
			out[i] = res.Text
			changed = changed || res.Text != v[i]
		}
		return out, changed, nil
	case map[string]any:
		changed := false
		out := make(map[string]any, len(v))
		for k, item := range v {
			if k == "image_url" || k == "url" || k == "b64_json" {
				out[k] = item
				continue
			}
			next, c, err := s.processAnyText(ctx, item)
			if err != nil {
				return value, changed, err
			}
			out[k] = next
			changed = changed || c
		}
		return out, changed, nil
	case []map[string]any:
		changed := false
		out := make([]map[string]any, len(v))
		for i := range v {
			next, c, err := s.processAnyText(ctx, v[i])
			if err != nil {
				return value, changed, err
			}
			m, _ := next.(map[string]any)
			out[i] = m
			changed = changed || c
		}
		return out, changed, nil
	default:
		return value, false, nil
	}
}
