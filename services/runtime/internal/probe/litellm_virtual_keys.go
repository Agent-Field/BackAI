// SPDX-License-Identifier: Apache-2.0

package probe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	LiteLLMVirtualKeysProbeID = "litellm-virtual-keys"
)

type LiteLLMVirtualKeysProbe struct {
	BaseURL    string
	MasterKey  string
	HTTPClient *http.Client
	Interval   time.Duration
}

func NewLiteLLMVirtualKeysProbe(baseURL, masterKey string, interval time.Duration) *LiteLLMVirtualKeysProbe {
	return &LiteLLMVirtualKeysProbe{
		BaseURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		MasterKey: masterKey,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		Interval: interval,
	}
}

func (p *LiteLLMVirtualKeysProbe) ID() string              { return LiteLLMVirtualKeysProbeID }
func (p *LiteLLMVirtualKeysProbe) Slot() string            { return "llm-chat" }
func (p *LiteLLMVirtualKeysProbe) Schedule() time.Duration { return p.Interval }

func (p *LiteLLMVirtualKeysProbe) Run(ctx context.Context) (Result, error) {
	res := Result{
		ProbeID:    p.ID(),
		Capability: "llm_gateway.virtual_keys_active",
		Value:      false,
		Severity:   SeverityUnavailable,
		LastRun:    time.Now().UTC(),
	}
	if strings.TrimSpace(p.BaseURL) == "" || strings.TrimSpace(p.MasterKey) == "" {
		res.Detail = "LiteLLM admin surface is not configured"
		return res, nil
	}
	q := url.Values{}
	q.Set("key_alias", "af-stack-capability-probe-never-created")
	endpoint := p.BaseURL + "/key/info?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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
	if resp.StatusCode < 400 || resp.StatusCode == http.StatusNotFound {
		res.Value = true
		res.Severity = SeverityOK
		res.Detail = "LiteLLM virtual-key admin surface is reachable"
		return res, nil
	}
	raw := string(body)
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "db not connected") || strings.Contains(lower, "database not connected") {
		res.Detail = "LiteLLM in stateless mode (/key/info returned DB not connected)"
		return res, nil
	}
	err = fmt.Errorf("LiteLLM /key/info returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(raw))
	res.Detail = err.Error()
	return res, err
}
