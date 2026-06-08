// SPDX-License-Identifier: Apache-2.0

// runs.go — #15 Realtime run subscriptions.
//
// Adds a WebSocket endpoint that streams live AgentField run events to
// SDK clients (suite.runs.subscribe(...)). The bridge consumes either:
//
//   - AgentField's per-execution SSE stream (single execution_id filter)
//   - AgentField's global UI execution-events SSE stream (filter-based)
//   - A 1Hz poll of /api/v1/executions/:id (fallback when AF is unreachable
//     or returns 404 for the SSE — e.g. UI module disabled)
//
// Why a new file instead of extending realtime.go:
//   - realtime.go is Postgres-LISTEN-backed, table-shaped (NOTIFY payloads
//     about row mutations). The run-event stream is AgentField-backed and
//     semantically different — separate file keeps each focused.
//   - Per the multi-item-plan, this needs to coexist with #25's additions
//     to agentfield/client.go and dashboard runs UI. Method names and
//     handler names here are unique on purpose.
//
// Wire protocol (server → client JSON over WebSocket):
//
//	{"type":"run.started",   "run_id":..., "execution_id":..., "agent":...,
//	 "node_id":..., "started_at":...}
//	{"type":"run.step",      "run_id":..., "execution_id":..., "step_id":...,
//	 "reasoner":..., "status":"running"|"done", "started_at":...,
//	 "ended_at":..., "tool_calls":[...], "tokens":{"input":N,"output":N}}
//	{"type":"run.completed", "run_id":..., "execution_id":...,
//	 "status":"succeeded"|"failed"|"cancelled", "duration_ms":N,
//	 "cost_usd":N}
//	{"type":"run.error",     "run_id":..., "error":...}
//	{"type":"server.degraded", "reason":...}  // emitted on AF unreachable
//
// Filter (query params; all optional):
//
//	tenant_id     — auto-bound to caller's tenant when multi-tenancy is on
//	user_id       — filter to one suite user
//	agent         — filter to one agent name (matches agent_node_id)
//	run_id        — filter to one AgentField run/workflow id
//	execution_id  — single-execution subscription (uses per-exec SSE path)
//
// See packages/sdk-py/af_stack/runs.py and packages/sdk-ts/src/runs.ts
// for the corresponding SDK surfaces.

package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
	"github.com/gorilla/websocket"
)

// runsSubscribeFilter is the parsed filter set the subscriber applies to
// each upstream event before forwarding to the WebSocket. All fields are
// optional; an empty filter matches every event the caller is allowed
// to see (subject to tenant scoping).
type runsSubscribeFilter struct {
	TenantID    string
	UserID      string
	Agent       string
	RunID       string
	ExecutionID string
	// RequireTenant is true when multi-tenancy is enabled — the
	// subscriber MUST then have a non-empty TenantID, and any inbound
	// event without a matching tenant is dropped.
	RequireTenant bool
}

// pinned reports whether the filter targets a single execution. When
// true the handler bridges the per-execution SSE stream instead of the
// global one.
func (f runsSubscribeFilter) pinned() bool {
	return strings.TrimSpace(f.ExecutionID) != ""
}

// parseRunsSubscribeFilter reads the URL query and the request context to
// build a filter. Tenant scoping is mandatory when multi-tenancy is on —
// any client-supplied tenant_id is ignored and overwritten with the
// caller's resolved tenant. With multi-tenancy off, the query value is
// trusted (local-dev / single-tenant deployments only).
func (s *Server) parseRunsSubscribeFilter(r *http.Request) runsSubscribeFilter {
	q := r.URL.Query()
	f := runsSubscribeFilter{
		TenantID:    strings.TrimSpace(q.Get("tenant_id")),
		UserID:      strings.TrimSpace(q.Get("user_id")),
		Agent:       strings.TrimSpace(q.Get("agent")),
		RunID:       strings.TrimSpace(q.Get("run_id")),
		ExecutionID: strings.TrimSpace(q.Get("execution_id")),
	}
	if s.multiTenancyEnabled() {
		f.RequireTenant = true
		// Force-bind to caller's resolved tenant — the SDK never
		// chooses which tenant it subscribes for.
		f.TenantID = tenantctx.TenantID(r.Context())
	}
	return f
}

// runEventWireMessage is the JSON envelope sent to subscribed WebSocket
// clients. Mirrors the table in this file's header comment.
type runEventWireMessage struct {
	Type        string         `json:"type"`
	RunID       string         `json:"run_id,omitempty"`
	ExecutionID string         `json:"execution_id,omitempty"`
	Agent       string         `json:"agent,omitempty"`
	NodeID      string         `json:"node_id,omitempty"`
	StepID      string         `json:"step_id,omitempty"`
	Reasoner    string         `json:"reasoner,omitempty"`
	Status      string         `json:"status,omitempty"`
	StartedAt   string         `json:"started_at,omitempty"`
	EndedAt     string         `json:"ended_at,omitempty"`
	UpdatedAt   string         `json:"updated_at,omitempty"`
	DurationMS  int64          `json:"duration_ms,omitempty"`
	CostUSD     float64        `json:"cost_usd,omitempty"`
	ToolCalls   []any          `json:"tool_calls,omitempty"`
	Tokens      map[string]any `json:"tokens,omitempty"`
	Error       string         `json:"error,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	// Raw is the original event payload from AgentField. Always
	// included so callers can rely on the structured fields for the
	// common case AND introspect everything else (e.g. trace ids) for
	// debugging. Kept as a map so JSON encoders preserve key order.
	Raw map[string]any `json:"raw,omitempty"`
}

// handleRunsSubscribe upgrades the request to a WebSocket and forwards
// matching run events from AgentField until the client disconnects, the
// context is cancelled, or the underlying stream ends.
//
// GET /api/v1/realtime/runs?[tenant_id|user_id|agent|run_id|execution_id]
func (s *Server) handleRunsSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.af == nil {
		writeError(w, http.StatusServiceUnavailable,
			"AGENTFIELD_NOT_CONFIGURED",
			"agentfield client not configured", nil)
		return
	}
	filter := s.parseRunsSubscribeFilter(r)
	if filter.RequireTenant && filter.TenantID == "" {
		writeError(w, http.StatusForbidden,
			"TENANT_REQUIRED",
			"realtime run subscriptions require a resolved tenant", nil)
		return
	}

	conn, err := realtimeUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Cancel the work loop the moment the client drops.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	// Pinned-to-one-execution: stream the per-exec SSE if available, then
	// emit a cached final snapshot if the run is already terminal.
	if filter.pinned() {
		s.streamRunsForExecution(ctx, conn, filter)
		return
	}

	// Filter-based: bridge the global UI stream; fall back to polling on
	// 404 or transport error. Polling targets any explicit run_id /
	// execution_id the caller provided so we don't hammer AF for
	// unconstrained subscriptions.
	s.streamRunsForFilter(ctx, conn, filter)
}

// streamRunsForExecution bridges the per-execution SSE stream for a
// single execution_id. When the AgentField stream is unavailable we
// fall back to a polled snapshot so the SDK still gets a final state.
func (s *Server) streamRunsForExecution(ctx context.Context, conn *websocket.Conn, f runsSubscribeFilter) {
	stream, err := s.af.StreamExecutionEventsByID(ctx, f.ExecutionID)
	if err != nil {
		s.emitRunsDegraded(conn, "agentfield unreachable: "+err.Error())
		s.emitFinalSnapshot(ctx, conn, f.ExecutionID)
		return
	}
	defer stream.Body.Close()

	if stream.StatusCode >= 400 {
		// 404 = AF didn't recognise the id; degrade gracefully and
		// poll once for a cached snapshot. Higher codes are
		// transient — same treatment, the client will reconnect.
		s.emitRunsDegraded(conn, "agentfield execution stream unavailable")
		s.emitFinalSnapshot(ctx, conn, f.ExecutionID)
		return
	}

	if err := s.relayRunsSSE(ctx, conn, stream.Body, f); err != nil &&
		!errors.Is(ctx.Err(), context.Canceled) {
		s.log.Warn("runs subscribe relay stopped",
			"execution_id", f.ExecutionID, "error", err)
	}
}

// streamRunsForFilter bridges the global UI execution-events stream and
// filters server-side. When AgentField's global stream is unavailable
// (UI module disabled → 404) we degrade gracefully; if a specific
// execution_id or run_id was supplied we additionally emit a polled
// snapshot so the client gets at least the current state.
func (s *Server) streamRunsForFilter(ctx context.Context, conn *websocket.Conn, f runsSubscribeFilter) {
	stream, err := s.af.StreamAllExecutionEvents(ctx)
	if err != nil {
		s.emitRunsDegraded(conn, "agentfield unreachable: "+err.Error())
		return
	}
	defer stream.Body.Close()

	if stream.StatusCode >= 400 {
		s.emitRunsDegraded(conn, "agentfield global event stream unavailable")
		return
	}

	if err := s.relayRunsSSE(ctx, conn, stream.Body, f); err != nil &&
		!errors.Is(ctx.Err(), context.Canceled) {
		s.log.Warn("runs subscribe global relay stopped", "error", err)
	}
}

// emitRunsDegraded sends a {"type":"server.degraded"} envelope. The
// caller is expected to either continue (fallback path engaged) or
// return; this never closes the socket on its own.
func (s *Server) emitRunsDegraded(conn *websocket.Conn, reason string) {
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_ = conn.WriteJSON(runEventWireMessage{
		Type:   "server.degraded",
		Reason: reason,
	})
}

// emitFinalSnapshot polls /executions/:id once and, if the run is
// terminal, emits a synthesised run.completed message. Used by the
// pinned-execution path so subscribers to already-finished runs see a
// closing event instead of hanging silently.
func (s *Server) emitFinalSnapshot(ctx context.Context, conn *websocket.Conn, executionID string) {
	if executionID == "" {
		return
	}
	pollCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	snap, err := s.af.PollExecutionSnapshot(pollCtx, executionID)
	if err != nil || snap.StatusCode >= 400 || snap.Status == "" {
		return
	}
	msg := snapshotToWireMessage(snap)
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_ = conn.WriteJSON(msg)
}

// snapshotToWireMessage maps a polled ExecutionSnapshot into a wire
// message. Terminal statuses become run.completed; otherwise run.step.
func snapshotToWireMessage(snap agentfield.ExecutionSnapshot) runEventWireMessage {
	msg := runEventWireMessage{
		ExecutionID: snap.ExecutionID,
		RunID:       snap.RunID,
		Agent:       snap.AgentNodeID,
		NodeID:      snap.AgentNodeID,
		Status:      normaliseRunStatus(snap.Status),
		UpdatedAt:   snap.UpdatedAt,
		DurationMS:  snap.DurationMS,
		Raw:         snap.Raw,
	}
	if snap.Terminal {
		msg.Type = "run.completed"
	} else {
		msg.Type = "run.step"
	}
	return msg
}

// normaliseRunStatus maps AgentField's many internal status strings into
// the run.completed wire enum: succeeded / failed / cancelled / running.
// Unknown values pass through so debug UIs still see them.
func normaliseRunStatus(status string) string {
	s := strings.ToLower(strings.TrimSpace(status))
	switch s {
	case "succeeded", "completed":
		return "succeeded"
	case "failed", "error":
		return "failed"
	case "cancelled", "canceled":
		return "cancelled"
	case "running", "started", "in_progress":
		return "running"
	}
	return s
}

// relayRunsSSE reads SSE frames off the upstream body, translates each
// ExecutionEvent JSON into the wire protocol, and ships matching
// messages over the WebSocket. Returns when the stream ends, the
// context cancels, or the WebSocket write fails.
//
// Heartbeats and connection-banner events are forwarded as no-ops
// (filtered out) so subscribers only see real run events.
func (s *Server) relayRunsSSE(ctx context.Context, conn *websocket.Conn, body io.Reader, f runsSubscribeFilter) error {
	scanner := bufio.NewScanner(body)
	buf := make([]byte, 0, 1<<16)
	scanner.Buffer(buf, 1<<20)

	var dataLines []string
	dispatch := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		raw := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil
		}
		msg, ok := runEventFromAgentFieldPayload(payload)
		if !ok {
			return nil
		}
		if !runEventMatchesFilter(msg, payload, f) {
			return nil
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(msg)
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			// SSE comment / heartbeat — drop.
			continue
		}
		if strings.HasPrefix(line, "event:") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return dispatch()
}

// runEventFromAgentFieldPayload maps AgentField's ExecutionEvent shape
// to our wire envelope. Returns false for non-run events (e.g. the
// "connected" hello, plain heartbeats) so the relay can skip them.
//
// Shape reminder (AgentField execution_events.go):
//
//	{
//	  "type": "execution_created|execution_started|...",
//	  "execution_id": "...",
//	  "workflow_id": "...",   // ← AgentField calls run_id "workflow_id"
//	  "agent_node_id": "...",
//	  "status": "...",
//	  "timestamp": "...",
//	  "data": { ... reasoner-specific extras ... }
//	}
func runEventFromAgentFieldPayload(p map[string]any) (runEventWireMessage, bool) {
	rawType, _ := p["type"].(string)
	switch rawType {
	case "", "connected", "heartbeat":
		return runEventWireMessage{}, false
	}

	msg := runEventWireMessage{Raw: p}
	if v, ok := p["execution_id"].(string); ok {
		msg.ExecutionID = v
	}
	if v, ok := p["workflow_id"].(string); ok {
		msg.RunID = v
	} else if v, ok := p["run_id"].(string); ok {
		msg.RunID = v
	}
	if v, ok := p["agent_node_id"].(string); ok {
		msg.Agent = v
		msg.NodeID = v
	}
	if v, ok := p["timestamp"].(string); ok {
		msg.UpdatedAt = v
	}
	if v, ok := p["status"].(string); ok {
		msg.Status = normaliseRunStatus(v)
	}

	// Reasoner-specific bits ride inside `data` for non-terminal events.
	if data, ok := p["data"].(map[string]any); ok {
		if v, ok := data["step_id"].(string); ok {
			msg.StepID = v
		}
		if v, ok := data["reasoner"].(string); ok {
			msg.Reasoner = v
		} else if v, ok := data["reasoner_id"].(string); ok {
			msg.Reasoner = v
		}
		if v, ok := data["started_at"].(string); ok {
			msg.StartedAt = v
		}
		if v, ok := data["ended_at"].(string); ok {
			msg.EndedAt = v
		}
		if v, ok := data["duration_ms"].(float64); ok {
			msg.DurationMS = int64(v)
		}
		if v, ok := data["cost_usd"].(float64); ok {
			msg.CostUSD = v
		}
		if v, ok := data["tool_calls"].([]any); ok {
			msg.ToolCalls = v
		}
		if v, ok := data["tokens"].(map[string]any); ok {
			msg.Tokens = v
		}
		if v, ok := data["error"].(string); ok {
			msg.Error = v
		}
	}

	switch rawType {
	case "execution_started", "execution_created":
		msg.Type = "run.started"
		if msg.StartedAt == "" {
			msg.StartedAt = msg.UpdatedAt
		}
	case "execution_completed":
		msg.Type = "run.completed"
		msg.Status = "succeeded"
	case "execution_failed":
		msg.Type = "run.completed"
		msg.Status = "failed"
		if msg.Error == "" {
			if data, ok := p["data"].(map[string]any); ok {
				if v, ok := data["error_message"].(string); ok {
					msg.Error = v
				}
			}
		}
	case "execution_cancelled":
		msg.Type = "run.completed"
		msg.Status = "cancelled"
	case "execution_updated", "execution_waiting", "execution_paused",
		"execution_resumed", "execution_approval_resolved", "execution_log",
		"execution_retried":
		msg.Type = "run.step"
	default:
		// Forward unknown semantic events as run.step so callers see
		// something rather than nothing; debug UIs can read Raw.
		msg.Type = "run.step"
	}
	return msg, true
}

// runEventMatchesFilter returns true iff the wire message satisfies the
// caller's filter. Tenant id is matched against the upstream payload's
// tenant_id field if present; AgentField doesn't always stamp tenant on
// individual events so the absence of tenant_id is treated as "any
// tenant" UNLESS RequireTenant is set (in which case we drop it — the
// caller can't safely receive untagged events under multi-tenancy).
func runEventMatchesFilter(msg runEventWireMessage, raw map[string]any, f runsSubscribeFilter) bool {
	if f.RunID != "" && msg.RunID != f.RunID {
		return false
	}
	if f.ExecutionID != "" && msg.ExecutionID != f.ExecutionID {
		return false
	}
	if f.Agent != "" && msg.Agent != f.Agent {
		return false
	}
	if f.UserID != "" {
		userID := stringField(raw, "user_id")
		if userID == "" {
			if data, ok := raw["data"].(map[string]any); ok {
				userID = stringField(data, "user_id")
			}
		}
		if userID != f.UserID {
			return false
		}
	}
	if f.RequireTenant {
		payloadTenant := stringField(raw, "tenant_id")
		if payloadTenant == "" {
			if data, ok := raw["data"].(map[string]any); ok {
				payloadTenant = stringField(data, "tenant_id")
			}
		}
		if payloadTenant == "" {
			// Drop untagged events when caller is tenant-scoped —
			// safer to under-deliver than to leak cross-tenant data.
			return false
		}
		if payloadTenant != f.TenantID {
			return false
		}
	} else if f.TenantID != "" {
		payloadTenant := stringField(raw, "tenant_id")
		if payloadTenant != "" && payloadTenant != f.TenantID {
			return false
		}
	}
	return true
}

// stringField is a tiny helper that returns m[k] as a string or "" when
// the field is missing or a different type. Kept private — used only
// inside this file.
func stringField(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
