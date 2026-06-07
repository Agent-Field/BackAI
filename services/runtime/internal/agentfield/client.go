// SPDX-License-Identifier: Apache-2.0

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

// AgentHarness mirrors the per-provider entry an agent emits from its
// __capabilities__ reasoner. Field names match the shape the harnesses
// aggregator expects (see services/runtime/internal/harnesses).
type AgentHarness struct {
	Provider    string   `json:"provider"`
	IsInstalled bool     `json:"is_installed"`
	BinaryPath  *string  `json:"binary_path"`
	Version     *string  `json:"version"`
	Status      string   `json:"status"` // ready|needs_auth|missing
	LastError   *string  `json:"last_error"`
	RequiredEnv []string `json:"required_env"`
}

// AgentMCPRunner is the per-runner entry from __capabilities__: name +
// resolved binary + (best-effort) version. Used to gate which agents
// can host stdio MCP servers spawned via uvx/npx.
type AgentMCPRunner struct {
	Name       string  `json:"name"`
	BinaryPath string  `json:"binary_path"`
	Version    *string `json:"version"`
}

// AgentCapabilities is the payload returned by an agent's
// __capabilities__ reasoner. Agents that don't define the reasoner are
// treated as having zero harnesses / runners.
type AgentCapabilities struct {
	NodeID     string           `json:"node_id"`
	Harnesses  []AgentHarness   `json:"harnesses"`
	MCPRunners []AgentMCPRunner `json:"mcp_runners"`
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

// Execute calls an AF agent reasoner synchronously.
//
// path is the AF endpoint path (e.g. "/api/v1/execute/sample.echo"); body
// is the request payload (already JSON-encoded). Returns AF's raw response
// body, status code, and the X-Execution-ID header if present.
func (c *Client) Execute(ctx context.Context, path string, body []byte) (ExecuteResponse, error) {
	if c.baseURL == "" {
		return ExecuteResponse{}, errors.New("agentfield: URL not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(string(body)))
	if err != nil {
		return ExecuteResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ExecuteResponse{}, fmt.Errorf("agentfield: execute %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return ExecuteResponse{
		StatusCode:  resp.StatusCode,
		Body:        respBody,
		ContentType: resp.Header.Get("Content-Type"),
		ExecutionID: resp.Header.Get("X-Execution-ID"),
	}, nil
}

// ExecuteResponse holds the raw response returned by AF for a sync execute.
type ExecuteResponse struct {
	StatusCode  int
	Body        []byte
	ContentType string
	ExecutionID string
}

// GetExecution queries the status of a previously-started async execution.
func (c *Client) GetExecution(ctx context.Context, id string) (ExecuteResponse, error) {
	if c.baseURL == "" {
		return ExecuteResponse{}, errors.New("agentfield: URL not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/executions/"+id, nil)
	if err != nil {
		return ExecuteResponse{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ExecuteResponse{}, fmt.Errorf("agentfield: get exec %s: %w", id, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return ExecuteResponse{
		StatusCode:  resp.StatusCode,
		Body:        body,
		ContentType: resp.Header.Get("Content-Type"),
	}, nil
}

// Capabilities calls every registered agent's ``__capabilities__``
// reasoner and returns one entry per agent.
//
// This is how the runtime learns what CLI harnesses and MCP runners
// live inside each AgentField agent container. The runtime container is
// distroless and intentionally cannot host those binaries; the
// harnesses / mcp packages aggregate the per-agent answers returned by
// this method.
//
// Agents that do not define ``__capabilities__`` are silently skipped
// (we treat them as "has no harnesses, has no runners"). Per-agent
// network errors are logged but do not fail the aggregate — a slow
// agent shouldn't take out the dashboard.
//
// The caller's ctx applies to the whole fan-out; pass a generous
// timeout (5–10s) so a single dead agent can't block the rest. Each
// per-agent call is also capped at 5 seconds.
func (c *Client) Capabilities(ctx context.Context) ([]AgentCapabilities, error) {
	agents, err := c.Discover(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AgentCapabilities, 0, len(agents))
	for _, a := range agents {
		if a.NodeID == "" {
			continue
		}
		caps, ok := c.fetchOneCapabilities(ctx, a.NodeID)
		if !ok {
			continue
		}
		if caps.NodeID == "" {
			caps.NodeID = a.NodeID
		}
		out = append(out, caps)
	}
	return out, nil
}

// fetchOneCapabilities calls a single agent's __capabilities__ reasoner.
// Returns (zero, false) when the reasoner is missing, the agent is
// unreachable, or the response isn't JSON we can parse. The runtime
// treats missing capabilities the same as an agent that declared none —
// not an error, just no harnesses / runners contributed.
func (c *Client) fetchOneCapabilities(ctx context.Context, nodeID string) (AgentCapabilities, bool) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := c.Execute(cctx, "/api/v1/execute/"+nodeID+".__capabilities__", []byte("{}"))
	if err != nil || resp.StatusCode >= 400 {
		return AgentCapabilities{}, false
	}
	// AgentField wraps reasoner output as { "status": "success",
	// "output": {...} }. We accept both the wrapped shape and a raw
	// capabilities object, since the wire shape evolved across SDK
	// versions.
	var wrapped struct {
		Output AgentCapabilities `json:"output"`
	}
	if err := json.Unmarshal(resp.Body, &wrapped); err == nil &&
		(wrapped.Output.NodeID != "" ||
			len(wrapped.Output.Harnesses) > 0 ||
			len(wrapped.Output.MCPRunners) > 0) {
		return wrapped.Output, true
	}
	var raw AgentCapabilities
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return AgentCapabilities{}, false
	}
	return raw, true
}

// CancelExecution cancels a running async execution.
func (c *Client) CancelExecution(ctx context.Context, id string) (ExecuteResponse, error) {
	if c.baseURL == "" {
		return ExecuteResponse{}, errors.New("agentfield: URL not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/v1/executions/"+id, nil)
	if err != nil {
		return ExecuteResponse{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ExecuteResponse{}, fmt.Errorf("agentfield: cancel %s: %w", id, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return ExecuteResponse{StatusCode: resp.StatusCode, Body: body}, nil
}