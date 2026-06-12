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
	"net/url"
	"os"
	"sort"
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
		Agents       []AgentInfo `json:"agents"`
		Capabilities []struct {
			AgentID   string `json:"agent_id"`
			Version   string `json:"version"`
			Reasoners []struct {
				ID   string   `json:"id"`
				Tags []string `json:"tags"`
			} `json:"reasoners"`
		} `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		// Tolerate AF's evolving schema for now.
		return []AgentInfo{}, nil
	}
	if raw.Agents != nil {
		return raw.Agents, nil
	}
	if raw.Capabilities == nil {
		return []AgentInfo{}, nil
	}
	agents := make([]AgentInfo, 0, len(raw.Capabilities))
	for _, capability := range raw.Capabilities {
		if capability.AgentID == "" {
			continue
		}
		reasoners := make([]string, 0, len(capability.Reasoners))
		tagSet := map[string]struct{}{}
		for _, reasoner := range capability.Reasoners {
			if reasoner.ID != "" {
				reasoners = append(reasoners, reasoner.ID)
			}
			for _, tag := range reasoner.Tags {
				if tag != "" {
					tagSet[tag] = struct{}{}
				}
			}
		}
		tags := make([]string, 0, len(tagSet))
		for tag := range tagSet {
			tags = append(tags, tag)
		}
		sort.Strings(tags)
		agents = append(agents, AgentInfo{
			NodeID:    capability.AgentID,
			Version:   capability.Version,
			Tags:      tags,
			Reasoners: reasoners,
		})
	}
	return agents, nil
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

// StreamResponse is a live response body returned by AgentField. The
// caller owns Body and must close it.
type StreamResponse struct {
	StatusCode  int
	Body        io.ReadCloser
	ContentType string
	Path        string
}

// StreamRunEvents opens AgentField's live workflow/run SSE stream.
//
// AF Stack uses this as a pass-through source for dashboard run viewers:
// AgentField remains the owner of execution events, spans, and workflow
// trace state. We try the current run-events path first, then the older
// workflow-events path documented by earlier AgentField releases.
func (c *Client) StreamRunEvents(ctx context.Context, runID string) (StreamResponse, error) {
	if c.baseURL == "" {
		return StreamResponse{}, errors.New("agentfield: URL not configured")
	}
	escaped := url.PathEscape(runID)
	candidates := []string{
		"/api/v1/workflows/runs/" + escaped + "/events/stream",
		"/api/v1/workflows/" + escaped + "/events",
	}
	streamClient := &http.Client{Timeout: 0}
	var last StreamResponse
	for _, path := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
		if err != nil {
			return StreamResponse{}, err
		}
		req.Header.Set("Accept", "text/event-stream")
		resp, err := streamClient.Do(req)
		if err != nil {
			return StreamResponse{}, fmt.Errorf("agentfield: stream run events %s: %w", runID, err)
		}
		current := StreamResponse{
			StatusCode:  resp.StatusCode,
			Body:        resp.Body,
			ContentType: resp.Header.Get("Content-Type"),
			Path:        path,
		}
		if resp.StatusCode == http.StatusNotFound {
			last = current
			_ = resp.Body.Close()
			continue
		}
		return current, nil
	}
	if last.Body != nil {
		last.Body = io.NopCloser(strings.NewReader(""))
		return last, nil
	}
	return StreamResponse{}, errors.New("agentfield: no run event stream path configured")
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

// Capabilities calls every registered agent's “__capabilities__“
// reasoner and returns one entry per agent.
//
// This is how the runtime learns what CLI harnesses and MCP runners
// live inside each AgentField agent container. The runtime container is
// distroless and intentionally cannot host those binaries; the
// harnesses / mcp packages aggregate the per-agent answers returned by
// this method.
//
// Agents that do not define “__capabilities__“ are silently skipped
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

// ─── #15 Realtime run subscriptions ────────────────────────────────────────
//
// Methods below back the suite.runs.subscribe(...) SDK surface.
//
// AgentField exposes two relevant streams:
//
//	GET /api/v1/executions/:execution_id/events  — per-execution SSE (canonical)
//	GET /api/ui/v1/executions/events             — global UI SSE (all executions)
//
// Strategy:
//   - Single execution_id → use the canonical per-execution stream.
//   - Filter-based (tenant / user / agent / run_id) → use the global stream and
//     filter server-side. We tolerate the global stream being unavailable
//     (e.g. UI module disabled) by falling back to polling individual runs.
//   - Polling fallback (PollExecutionSnapshot) is also used to emit a final
//     cached snapshot for finished runs subscribed by execution_id.
//
// Method names are deliberately unique vs. the existing StreamRunEvents /
// GetExecution so this can be merged additively alongside item #25.

// ExecutionSnapshot is the polled view of an execution's current state.
// Returned by PollExecutionSnapshot; used by the runs subscriber to emit
// run.started / run.completed events when the SSE stream isn't available.
type ExecutionSnapshot struct {
	// Raw is the verbatim JSON body from /executions/:id. The runs
	// subscriber re-shapes selected fields into wire-protocol messages.
	Raw map[string]any
	// Convenience fields parsed out of Raw. All are best-effort — if a
	// field is missing the subscriber falls through to neutral defaults.
	ExecutionID string
	RunID       string
	AgentNodeID string
	Status      string
	Terminal    bool
	UpdatedAt   string
	DurationMS  int64
	StatusCode  int
}

// StreamExecutionEventsByID opens the per-execution SSE stream against
// /api/v1/executions/:execution_id/events. Used by the runs subscriber
// when the caller pinned the subscription to a single execution_id.
//
// Distinct from StreamRunEvents (which targets the older workflow-events
// path) — added in #15 so the two callers can evolve independently.
func (c *Client) StreamExecutionEventsByID(ctx context.Context, executionID string) (StreamResponse, error) {
	if c.baseURL == "" {
		return StreamResponse{}, errors.New("agentfield: URL not configured")
	}
	path := "/api/v1/executions/" + url.PathEscape(executionID) + "/events"
	return c.openSSEStream(ctx, path)
}

// StreamAllExecutionEvents opens AgentField's global UI execution-events
// SSE stream. Emits ExecutionEvent JSON for every running execution in the
// AgentField instance. The runs subscriber filters server-side by tenant,
// user, agent, and run_id before forwarding to a WebSocket client.
//
// Returns a non-nil StreamResponse with StatusCode == 404 when the AF UI
// module is disabled (a small but real production case). Callers MUST
// handle that and fall back to polling.
func (c *Client) StreamAllExecutionEvents(ctx context.Context) (StreamResponse, error) {
	if c.baseURL == "" {
		return StreamResponse{}, errors.New("agentfield: URL not configured")
	}
	return c.openSSEStream(ctx, "/api/ui/v1/executions/events")
}

// openSSEStream is the shared GET-with-Accept-event-stream helper used by
// the two new SSE methods. Kept private so the public surface stays small.
func (c *Client) openSSEStream(ctx context.Context, path string) (StreamResponse, error) {
	streamClient := &http.Client{Timeout: 0}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return StreamResponse{}, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := streamClient.Do(req)
	if err != nil {
		return StreamResponse{}, fmt.Errorf("agentfield: open SSE %s: %w", path, err)
	}
	return StreamResponse{
		StatusCode:  resp.StatusCode,
		Body:        resp.Body,
		ContentType: resp.Header.Get("Content-Type"),
		Path:        path,
	}, nil
}

// PollExecutionSnapshot fetches /api/v1/executions/:id and parses the
// shape into an ExecutionSnapshot. Used by the runs subscriber as a
// polling fallback when the SSE stream is unavailable, and to emit a
// final cached state for already-completed runs.
//
// Returns a snapshot with StatusCode preserved so the caller can
// distinguish 404 (run not found) from transport failures (returned as
// the error). Body is read but never returned raw — the caller wants
// structured fields, not bytes.
func (c *Client) PollExecutionSnapshot(ctx context.Context, executionID string) (ExecutionSnapshot, error) {
	if c.baseURL == "" {
		return ExecutionSnapshot{}, errors.New("agentfield: URL not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/executions/"+url.PathEscape(executionID), nil)
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ExecutionSnapshot{}, fmt.Errorf("agentfield: poll %s: %w", executionID, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	snap := ExecutionSnapshot{StatusCode: resp.StatusCode, ExecutionID: executionID}
	if resp.StatusCode >= 400 {
		return snap, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err == nil {
		snap.Raw = raw
		if v, ok := raw["execution_id"].(string); ok && v != "" {
			snap.ExecutionID = v
		}
		if v, ok := raw["run_id"].(string); ok {
			snap.RunID = v
		} else if v, ok := raw["workflow_id"].(string); ok {
			snap.RunID = v
		}
		if v, ok := raw["agent_node_id"].(string); ok {
			snap.AgentNodeID = v
		}
		if v, ok := raw["status"].(string); ok {
			snap.Status = v
			snap.Terminal = isTerminalExecutionStatus(v)
		}
		if v, ok := raw["updated_at"].(string); ok {
			snap.UpdatedAt = v
		}
		if v, ok := raw["duration_ms"].(float64); ok {
			snap.DurationMS = int64(v)
		}
	}
	return snap, nil
}

// isTerminalExecutionStatus mirrors AgentField's types.IsTerminalExecutionStatus
// — kept local so the runtime doesn't depend on AF's internal Go packages.
// Statuses are lower-cased before comparison.
func isTerminalExecutionStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded", "completed", "failed", "cancelled", "canceled", "error":
		return true
	}
	return false
}

// ─── #25 AgentField data in dashboard ─────────────────────────────────────
//
// Methods below proxy to AgentField's control-plane "agent-api" surface
// (the same endpoints AgentField's own UI at :8081 uses for the run / DAG
// inspector). They power the af-stack dashboard's run detail page —
// summary card inline + link-out to AgentField for the deep view.
//
// Naming note: the existing CancelExecution (above) targets the older
// `/api/v1/executions/:id` (DELETE) surface used by async execute. The
// agent-api endpoints are POST against `/agent-api/executions/:id/cancel`
// (and similar) and carry richer semantics (pause / resume / approval
// gating). To stay additive we expose them under distinct names — reads
// are `Get*`, control verbs carry an `Agent` infix so the two cancel
// surfaces never collide.

// RunOverview is the af-stack-side projection of AgentField's
// `GET /agentic/run/:run_id` response.
//
// We only model the fields the dashboard's summary card reads. Anything
// the dashboard doesn't render stays in Extra (raw JSON) so additive
// AgentField schema changes don't require a runtime release.
type RunOverview struct {
	ExecutionID    string  `json:"execution_id"`
	RunID          string  `json:"run_id,omitempty"`
	Status         string  `json:"status"`
	AgentName      string  `json:"agent_name,omitempty"`
	Reasoner       string  `json:"reasoner,omitempty"`
	StartedAt      string  `json:"started_at,omitempty"`
	EndedAt        string  `json:"ended_at,omitempty"`
	DurationMS     *int64  `json:"duration_ms,omitempty"`
	CostUSD        float64 `json:"cost_usd"`
	ApprovalStatus string  `json:"approval_status,omitempty"`
	// Extra holds the raw upstream response so the dashboard can render
	// fields we don't yet model without a runtime release.
	Extra json.RawMessage `json:"extra,omitempty"`
}

// GetRunOverview proxies AgentField's `GET /agentic/run/:run_id`.
//
// Returns the live run state (status, agent, timing, cost, approval).
// Used by the af-stack dashboard's run-detail summary card. Tolerates
// AgentField returning a partially-populated body — the dashboard
// degrades gracefully on missing fields.
func (c *Client) GetRunOverview(ctx context.Context, runID string) (RunOverview, error) {
	if c.baseURL == "" {
		return RunOverview{}, errors.New("agentfield: URL not configured")
	}
	if runID == "" {
		return RunOverview{}, errors.New("agentfield: run id required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/agentic/run/"+url.PathEscape(runID), nil)
	if err != nil {
		return RunOverview{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return RunOverview{}, fmt.Errorf("agentfield: get run overview %s: %w", runID, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusNotFound {
		return RunOverview{}, fmt.Errorf("agentfield: run %s not found", runID)
	}
	if resp.StatusCode >= 400 {
		return RunOverview{}, fmt.Errorf("agentfield: get run overview status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out RunOverview
	// Tolerate AgentField's evolving wire shape: decode the known fields,
	// stash the raw payload under Extra for downstream renderers.
	_ = json.Unmarshal(body, &out)
	if out.ExecutionID == "" {
		// Some AgentField versions key the response by run_id only. Fall
		// back to the path id so the dashboard always has a stable handle.
		out.ExecutionID = out.RunID
	}
	if out.RunID == "" {
		out.RunID = runID
	}
	out.Extra = json.RawMessage(body)
	return out, nil
}

// GetExecutionDetails proxies `GET /agent-api/executions/:execution_id/details`.
//
// Returns the raw JSON body — AgentField's own UI uses this endpoint for
// its deep run inspector. The af-stack dashboard does not parse it; the
// shape is rich and evolving (spans, tool I/O, step graph). We expose it
// for power-users and forward-compat without mirroring the schema as Go
// structs.
func (c *Client) GetExecutionDetails(ctx context.Context, executionID string) (json.RawMessage, error) {
	if c.baseURL == "" {
		return nil, errors.New("agentfield: URL not configured")
	}
	if executionID == "" {
		return nil, errors.New("agentfield: execution id required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/agent-api/executions/"+url.PathEscape(executionID)+"/details", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agentfield: get execution details %s: %w", executionID, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("agentfield: execution %s not found", executionID)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("agentfield: get execution details status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.RawMessage(body), nil
}

// postAgentAPI is the shared helper for the cancel / pause / resume /
// request-approval POSTs against the agent-api surface. The endpoints
// share an identical request shape (empty body) and we treat any 2xx as
// success.
func (c *Client) postAgentAPI(ctx context.Context, executionID, action string) error {
	if c.baseURL == "" {
		return errors.New("agentfield: URL not configured")
	}
	if executionID == "" {
		return errors.New("agentfield: execution id required")
	}
	path := c.baseURL + "/agent-api/executions/" + url.PathEscape(executionID) + "/" + action
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("agentfield: %s %s: %w", action, executionID, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("agentfield: execution %s not found", executionID)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("agentfield: %s status %d: %s",
			action, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// CancelAgentExecution proxies `POST /agent-api/executions/:id/cancel`.
//
// Distinct from the older CancelExecution above (which uses the legacy
// `/api/v1/executions/:id` DELETE shape). This newer surface is what
// AgentField's UI uses and is required for runs that may be in an
// awaiting-approval state.
func (c *Client) CancelAgentExecution(ctx context.Context, executionID string) error {
	return c.postAgentAPI(ctx, executionID, "cancel")
}

// PauseAgentExecution proxies `POST /agent-api/executions/:id/pause`.
func (c *Client) PauseAgentExecution(ctx context.Context, executionID string) error {
	return c.postAgentAPI(ctx, executionID, "pause")
}

// ResumeAgentExecution proxies `POST /agent-api/executions/:id/resume`.
func (c *Client) ResumeAgentExecution(ctx context.Context, executionID string) error {
	return c.postAgentAPI(ctx, executionID, "resume")
}

// RequestAgentApproval proxies `POST /agent-api/executions/:id/request-approval`.
//
// Used to escalate a run into AgentField's approval flow (operator must
// explicitly approve before subsequent steps run). Pairs with the
// approval_status field returned by GetRunOverview.
func (c *Client) RequestAgentApproval(ctx context.Context, executionID string) error {
	return c.postAgentAPI(ctx, executionID, "request-approval")
}

// AgentFieldURL returns the customer-facing URL of AgentField's UI used
// for "View in AgentField" link-outs from the dashboard.
//
// Read order:
//
//  1. AF_STACK_AGENTFIELD_PUBLIC_URL — set this in prod to AgentField's
//     externally-reachable URL (e.g. https://af.example.com).
//  2. The configured baseURL — falls back to the same host we use for
//     server-to-server calls. In dev (docker-compose) that's typically
//     `http://localhost:8081` already.
//
// Never returns the empty string; callers can render the result directly.
func (c *Client) AgentFieldURL() string {
	if v := strings.TrimRight(os.Getenv("AF_STACK_AGENTFIELD_PUBLIC_URL"), "/"); v != "" {
		return v
	}
	if c.baseURL != "" {
		return c.baseURL
	}
	return "http://localhost:8081"
}
