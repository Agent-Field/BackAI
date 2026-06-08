// SPDX-License-Identifier: Apache-2.0

package guardrails

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type presidioClient struct {
	analyzerURL   string
	anonymizerURL string
	httpClient    *http.Client
}

type presidioAnalyzeRequest struct {
	Text     string   `json:"text"`
	Language string   `json:"language"`
	Entities []string `json:"entities,omitempty"`
}

type presidioResult struct {
	EntityType string  `json:"entity_type"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Score      float64 `json:"score"`
}

type presidioAnonymizeRequest struct {
	Text            string                    `json:"text"`
	AnalyzerResults []presidioResult          `json:"analyzer_results"`
	Anonymizers     map[string]map[string]any `json:"anonymizers,omitempty"`
}

type presidioAnonymizeResponse struct {
	Text  string          `json:"text"`
	Items json.RawMessage `json:"items,omitempty"`
}

func newPresidioClient(cfg Config) *presidioClient {
	analyzerURL := strings.TrimRight(strings.TrimSpace(cfg.PresidioAnalyzerURL), "/")
	anonymizerURL := strings.TrimRight(strings.TrimSpace(cfg.PresidioAnonymizerURL), "/")
	if analyzerURL == "" {
		return nil
	}
	if anonymizerURL == "" {
		anonymizerURL = analyzerURL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &presidioClient{
		analyzerURL:   analyzerURL,
		anonymizerURL: anonymizerURL,
		httpClient:    client,
	}
}

func (c *presidioClient) redact(ctx context.Context, text string) (string, []Finding, bool, error) {
	if c == nil {
		return "", nil, false, fmt.Errorf("presidio client is not configured")
	}
	results, err := c.analyze(ctx, text)
	if err != nil {
		return "", nil, false, err
	}
	if len(results) == 0 {
		return text, nil, false, nil
	}
	req := presidioAnonymizeRequest{
		Text:            text,
		AnalyzerResults: results,
		Anonymizers: map[string]map[string]any{
			"DEFAULT": {
				"type":      "replace",
				"new_value": "[REDACTED_PII]",
			},
		},
	}
	var out presidioAnonymizeResponse
	if err := c.postJSON(ctx, c.anonymizerURL+"/anonymize", req, &out); err != nil {
		return "", nil, false, err
	}
	findings := make([]Finding, 0, len(results))
	for _, r := range results {
		findings = append(findings, Finding{
			Type:   r.EntityType,
			Start:  r.Start,
			End:    r.End,
			Source: ProviderPresidio,
		})
	}
	if out.Text == "" {
		out.Text = text
	}
	return out.Text, findings, out.Text != text, nil
}

func (c *presidioClient) analyze(ctx context.Context, text string) ([]presidioResult, error) {
	req := presidioAnalyzeRequest{Text: text, Language: "en"}
	var out []presidioResult
	if err := c.postJSON(ctx, c.analyzerURL+"/analyze", req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *presidioClient) postJSON(ctx context.Context, url string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("presidio status %d: %s", resp.StatusCode, string(respBody))
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return err
	}
	return nil
}
