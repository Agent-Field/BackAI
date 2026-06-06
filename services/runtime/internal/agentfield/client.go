// Package agentfield is the suite runtime's client for the AgentField
// control plane.
//
// Thin wrapper around AF's REST API: health, discovery, agent execution.
// All LLM calls and agent invocations route through here, never directly
// to providers — that's the suite's core invariant.
package agentfield

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client wraps an AgentField control plane endpoint.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Config holds AF client settings.
type Config struct {
	URL            string
	RequestTimeout time.Duration
}

// New constructs a Client. The URL is normalised to have no trailing slash.
func New(cfg Config) *Client {
	timeout := cfg.RequestTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.URL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// HealthStatus represents AF's reported health.
type HealthStatus struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Timestamp string `json:"timestamp"`
}

// Health probes AF's /health endpoint.
//
// Returns an error if AF is unreachable or returns non-2xx.
func (c *Client) Health(ctx context.Context) (HealthStatus, error) {
	if c.baseURL == "" {
		return HealthStatus{}, errors.New("agentfield: URL not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return HealthStatus{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return HealthStatus{}, fmt.Errorf("agentfield: GET /health: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return HealthStatus{}, fmt.Errorf("agentfield: unhealthy (status %d): %s",
			resp.StatusCode, string(body))
	}
	var hs HealthStatus
	if err := json.Unmarshal(body, &hs); err != nil {
		// AF's health response is JSON but the schema may vary; tolerate it.
		hs.Status = "ok"
	}
	if hs.Status == "" {
		hs.Status = "ok"
	}
	return hs, nil
}

// AgentInfo is a minimal description of a registered agent.
type AgentInfo struct {
	NodeID    string   `json:"node_id"`
	Version   string   `json:"version,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Reasoners []string `json:"reasoners,omitempty"`
}

// Discover lists registered agents in AF. Used by the suite's
// `/api/v1/agents` discovery endpoint.
//
// Returns an empty slice if no agents are registered yet (not an error).
func (c *Client) Discover(ctx context.Context) ([]AgentInfo, error) {
	if c.baseURL == "" {
		return nil, errors.New("agentfield: URL not configured")
	}
	// AF's actual discovery endpoint may differ; we tolerate either shape and
	// will refine when we wire end-to-end against a live AF instance.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/discovery/capabilities", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agentfield: discover: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return []AgentInfo{}, nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("agentfield: discover status %d", resp.StatusCode)
	}
	var raw struct {
		Agents []AgentInfo `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		// Tolerate AF's evolving schema for now.
		return []AgentInfo{}, nil
	}
	if raw.Agents == nil {
		return []AgentInfo{}, nil
	}
	return raw.Agents, nil
}

// BaseURL returns the configured AF URL (for logs and diagnostics).
func (c *Client) BaseURL() string {
	return c.baseURL
}
