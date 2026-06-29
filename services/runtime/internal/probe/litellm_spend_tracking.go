// SPDX-License-Identifier: Apache-2.0

package probe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const LiteLLMSpendTrackingProbeID = "litellm-spend-tracking"

type LiteLLMSpendTrackingProbe struct {
	BaseURL    string
	MasterKey  string
	HTTPClient *http.Client
	Interval   time.Duration
}

func NewLiteLLMSpendTrackingProbe(baseURL, masterKey string, interval time.Duration) *LiteLLMSpendTrackingProbe {
	return &LiteLLMSpendTrackingProbe{
		BaseURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		MasterKey: masterKey,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		Interval: interval,
	}
}

func (p *LiteLLMSpendTrackingProbe) ID() string              { return LiteLLMSpendTrackingProbeID }
func (p *LiteLLMSpendTrackingProbe) Slot() string            { return "llm-chat" }
func (p *LiteLLMSpendTrackingProbe) Schedule() time.Duration { return p.Interval }

func (p *LiteLLMSpendTrackingProbe) Run(ctx context.Context) (Result, error) {
	res := Result{
		ProbeID:    p.ID(),
		Capability: "llm_gateway.spend_tracking_active",
		Value:      false,
		Severity:   SeverityUnavailable,
		LastRun:    time.Now().UTC(),
	}
	if strings.TrimSpace(p.BaseURL) == "" || strings.TrimSpace(p.MasterKey) == "" {
		res.Detail = "LiteLLM admin surface is not configured"
		return res, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/spend/keys", nil)
	if err != nil {
		res.Detail = err.Error()
		return res, err
	}
	req.Header.Set("Authorization", "Bearer "+p.MasterKey)
	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		res.Detail = err.Error()
		return res, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		res.Value = true
		res.Severity = SeverityOK
		res.Detail = "LiteLLM spend endpoint is reachable"
		return res, nil
	}
	err = fmt.Errorf("LiteLLM /spend/keys returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	res.Detail = err.Error()
	return res, err
}
