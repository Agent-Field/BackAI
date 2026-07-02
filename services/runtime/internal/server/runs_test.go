// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/gorilla/websocket"
)

// TestRunEventFromAgentFieldPayload covers the SSE → wire-protocol
// translation: each AgentField execution event maps to a wire `type`
// and the run identifiers / status flow through correctly.
func TestRunEventFromAgentFieldPayload(t *testing.T) {
	cases := []struct {
		name        string
		payload     map[string]any
		wantType    string
		wantStatus  string
		wantRunID   string
		wantExecID  string
		wantAgent   string
		wantSkipped bool
	}{
		{
			name: "execution_started maps to run.started",
			payload: map[string]any{
				"type":          "execution_started",
				"execution_id":  "exec-1",
				"workflow_id":   "run-1",
				"agent_node_id": "notable-ai.summarize",
				"status":        "running",
			},
			wantType:   "run.started",
			wantStatus: "running",
			wantRunID:  "run-1",
			wantExecID: "exec-1",
			wantAgent:  "notable-ai.summarize",
		},
		{
			name: "execution_completed maps to run.completed succeeded",
			payload: map[string]any{
				"type":         "execution_completed",
				"execution_id": "exec-2",
				"workflow_id":  "run-2",
				"status":       "succeeded",
			},
			wantType:   "run.completed",
			wantStatus: "succeeded",
			wantExecID: "exec-2",
		},
		{
			name: "execution_failed maps to run.completed failed",
			payload: map[string]any{
				"type":         "execution_failed",
				"execution_id": "exec-3",
				"status":       "failed",
				"data": map[string]any{
					"error_message": "boom",
				},
			},
			wantType:   "run.completed",
			wantStatus: "failed",
			wantExecID: "exec-3",
		},
		{
			name: "execution_cancelled maps to run.completed cancelled",
			payload: map[string]any{
				"type":         "execution_cancelled",
				"execution_id": "exec-4",
				"status":       "cancelled",
			},
			wantType:   "run.completed",
			wantStatus: "cancelled",
			wantExecID: "exec-4",
		},
		{
			name: "execution_updated maps to run.step",
			payload: map[string]any{
				"type":         "execution_updated",
				"execution_id": "exec-5",
				"workflow_id":  "run-5",
				"status":       "running",
				"data": map[string]any{
					"step_id":  "step-1",
					"reasoner": "summarize",
				},
			},
			wantType:   "run.step",
			wantStatus: "running",
			wantRunID:  "run-5",
			wantExecID: "exec-5",
		},
		{
			name:        "connected hello is skipped",
			payload:     map[string]any{"type": "connected", "message": "hi"},
			wantSkipped: true,
		},
		{
			name:        "heartbeat is skipped",
			payload:     map[string]any{"type": "heartbeat"},
			wantSkipped: true,
		},
		{
			name:        "missing type is skipped",
			payload:     map[string]any{"execution_id": "exec-9"},
			wantSkipped: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := runEventFromAgentFieldPayload(tc.payload)
			if tc.wantSkipped {
				if ok {
					t.Fatalf("expected payload to be skipped, got %+v", msg)
				}
				return
			}
			if !ok {
				t.Fatalf("expected ok, got skipped")
			}
			if msg.Type != tc.wantType {
				t.Fatalf("type = %q, want %q", msg.Type, tc.wantType)
			}
			if tc.wantStatus != "" && msg.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", msg.Status, tc.wantStatus)
			}
			if tc.wantRunID != "" && msg.RunID != tc.wantRunID {
				t.Fatalf("run_id = %q, want %q", msg.RunID, tc.wantRunID)
			}
			if tc.wantExecID != "" && msg.ExecutionID != tc.wantExecID {
				t.Fatalf("execution_id = %q, want %q", msg.ExecutionID, tc.wantExecID)
			}
			if tc.wantAgent != "" && msg.Agent != tc.wantAgent {
				t.Fatalf("agent = %q, want %q", msg.Agent, tc.wantAgent)
			}
		})
	}
}

// TestRunEventMatchesFilter verifies the server-side filter is strict:
// run/agent/user/tenant predicates all hold, and tenant scoping under
// multi-tenancy drops untagged or cross-tenant events.
func TestRunEventMatchesFilter(t *testing.T) {
	makeRaw := func(extras ...func(map[string]any)) map[string]any {
		raw := map[string]any{
			"type":          "execution_updated",
			"execution_id":  "exec-1",
			"workflow_id":   "run-1",
			"agent_node_id": "notable-ai.summarize",
			"tenant_id":     "tenant-a",
			"user_id":       "u-1",
		}
		for _, f := range extras {
			f(raw)
		}
		return raw
	}
	msg, ok := runEventFromAgentFieldPayload(makeRaw())
	if !ok {
		t.Fatal("base payload should parse")
	}

	if !runEventMatchesFilter(msg, makeRaw(), runsSubscribeFilter{}) {
		t.Fatal("empty filter should match")
	}
	if !runEventMatchesFilter(msg, makeRaw(), runsSubscribeFilter{RunID: "run-1"}) {
		t.Fatal("matching run_id should match")
	}
	if runEventMatchesFilter(msg, makeRaw(), runsSubscribeFilter{RunID: "other"}) {
		t.Fatal("non-matching run_id should not match")
	}
	if runEventMatchesFilter(msg, makeRaw(), runsSubscribeFilter{Agent: "other"}) {
		t.Fatal("non-matching agent should not match")
	}
	if runEventMatchesFilter(msg, makeRaw(), runsSubscribeFilter{UserID: "u-other"}) {
		t.Fatal("non-matching user should not match")
	}

	// Tenant scoping: caller is tenant-a; payload from tenant-b drops.
	if runEventMatchesFilter(msg, makeRaw(func(m map[string]any) {
		m["tenant_id"] = "tenant-b"
	}), runsSubscribeFilter{TenantID: "tenant-a", RequireTenant: true}) {
		t.Fatal("cross-tenant event should be dropped under multi-tenancy")
	}

	// Tenant scoping: untagged events drop too.
	if runEventMatchesFilter(msg, makeRaw(func(m map[string]any) {
		delete(m, "tenant_id")
	}), runsSubscribeFilter{TenantID: "tenant-a", RequireTenant: true}) {
		t.Fatal("untagged event should be dropped under multi-tenancy")
	}

	// With multi-tenancy off, untagged events pass.
	if !runEventMatchesFilter(msg, makeRaw(func(m map[string]any) {
		delete(m, "tenant_id")
	}), runsSubscribeFilter{TenantID: "tenant-a"}) {
		t.Fatal("untagged event should pass without multi-tenancy")
	}
}

// TestRunsSubscribeFilterPinned exercises the pinned() check that
// drives the per-execution SSE path vs. global stream selection.
func TestRunsSubscribeFilterPinned(t *testing.T) {
	if (runsSubscribeFilter{}).pinned() {
		t.Fatal("empty filter should not be pinned")
	}
	if !(runsSubscribeFilter{ExecutionID: "exec-1"}).pinned() {
		t.Fatal("execution_id filter should be pinned")
	}
}

// TestParseRunsSubscribeFilterTrimsAndScopes verifies query parsing.
func TestParseRunsSubscribeFilterTrimsAndScopes(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/realtime/runs?tenant_id=t1&user_id=u1&agent=a1&run_id=r1&execution_id=e1", nil)
	f := s.parseRunsSubscribeFilter(req)
	if f.TenantID != "t1" || f.UserID != "u1" || f.Agent != "a1" ||
		f.RunID != "r1" || f.ExecutionID != "e1" {
		t.Fatalf("unexpected parsed filter: %+v", f)
	}
	if f.RequireTenant {
		t.Fatal("multi-tenancy is off in this test server, RequireTenant should be false")
	}
}

// TestSnapshotToWireMessage verifies the polled-snapshot path emits a
// well-formed run.completed for terminal runs and run.step otherwise.
func TestSnapshotToWireMessage(t *testing.T) {
	terminal := snapshotToWireMessage(agentfield.ExecutionSnapshot{
		ExecutionID: "exec-1",
		RunID:       "run-1",
		AgentNodeID: "agent-a",
		Status:      "succeeded",
		Terminal:    true,
		UpdatedAt:   "2026-06-07T00:00:00Z",
		DurationMS:  123,
	})
	if terminal.Type != "run.completed" {
		t.Fatalf("terminal snapshot type = %q", terminal.Type)
	}
	if terminal.Status != "succeeded" {
		t.Fatalf("terminal snapshot status = %q", terminal.Status)
	}
	mid := snapshotToWireMessage(agentfield.ExecutionSnapshot{
		ExecutionID: "exec-2",
		Status:      "running",
	})
	if mid.Type != "run.step" {
		t.Fatalf("non-terminal snapshot type = %q", mid.Type)
	}
}

// TestHandleRunsSubscribeRelaysAndFilters is a small integration test:
// stand up a fake AgentField SSE server, point the Server.af client at
// it, open the WebSocket subscription via the runtime mux, and verify
// matching events round-trip while non-matching ones get filtered.
func TestHandleRunsSubscribeRelaysAndFilters(t *testing.T) {
	// 1. Fake AgentField — answers the global UI events path with
	//    several execution events; only those for run-target should
	//    reach the subscriber.
	af := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ui/v1/executions/events" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeEvent := func(payload map[string]any) {
			body, _ := json.Marshal(payload)
			_, _ = w.Write([]byte("event: execution\ndata: "))
			_, _ = w.Write(body)
			_, _ = w.Write([]byte("\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
		writeEvent(map[string]any{"type": "connected"})
		writeEvent(map[string]any{
			"type":          "execution_started",
			"execution_id":  "exec-target",
			"workflow_id":   "run-target",
			"agent_node_id": "agent-x",
			"status":        "running",
		})
		writeEvent(map[string]any{
			"type":          "execution_started",
			"execution_id":  "exec-other",
			"workflow_id":   "run-other",
			"agent_node_id": "agent-y",
			"status":        "running",
		})
		writeEvent(map[string]any{
			"type":          "execution_completed",
			"execution_id":  "exec-target",
			"workflow_id":   "run-target",
			"agent_node_id": "agent-x",
			"status":        "succeeded",
		})
		// Hold the response open briefly so the runtime's scanner
		// drains everything before we close.
		time.Sleep(200 * time.Millisecond)
	}))
	defer af.Close()

	srv := newTestServerForRuns(t, af.URL)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) +
		"/api/v1/realtime/runs?run_id=run-target"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	want := []struct {
		runID, evtType string
	}{
		{"run-target", "run.started"},
		{"run-target", "run.completed"},
	}
	for i, w := range want {
		var got runEventWireMessage
		if err := conn.ReadJSON(&got); err != nil {
			t.Fatalf("read[%d]: %v", i, err)
		}
		if got.RunID != w.runID {
			t.Fatalf("read[%d] run_id = %q, want %q", i, got.RunID, w.runID)
		}
		if got.Type != w.evtType {
			t.Fatalf("read[%d] type = %q, want %q", i, got.Type, w.evtType)
		}
	}
}

// TestHandleRunsSubscribeAFUnreachable verifies the WebSocket emits
// server.degraded when AgentField is down instead of crashing.
func TestHandleRunsSubscribeAFUnreachable(t *testing.T) {
	srv := newTestServerForRuns(t, closedLoopbackURL(t)) // just-closed port (refuses fast on any OS)

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) +
		"/api/v1/realtime/runs?run_id=run-x"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var msg runEventWireMessage
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read: %v", err)
	}
	if msg.Type != "server.degraded" {
		t.Fatalf("type = %q, want server.degraded", msg.Type)
	}
	if msg.Reason == "" {
		t.Fatal("expected non-empty degraded reason")
	}
}

// TestHandleRunsSubscribeAgentFieldNotConfigured asserts the handler
// returns 503 when there's no AgentField client wired at all.
func TestHandleRunsSubscribeAgentFieldNotConfigured(t *testing.T) {
	s := &Server{mux: http.NewServeMux(), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	s.mux.HandleFunc("GET /api/v1/realtime/runs", s.handleRunsSubscribe)

	ts := httptest.NewServer(s.mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/realtime/runs?run_id=r1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("AGENTFIELD_NOT_CONFIGURED")) {
		t.Fatalf("body = %s, want AGENTFIELD_NOT_CONFIGURED", body)
	}
}

// newTestServerForRuns builds the smallest Server value that handles
// /api/v1/realtime/runs. Kept private to this test file so the
// production constructor stays unchanged.
func newTestServerForRuns(t *testing.T, afURL string) *Server {
	t.Helper()
	client := agentfield.New(agentfield.Config{
		URL:            afURL,
		RequestTimeout: 5 * time.Second,
	})
	s := &Server{
		mux: http.NewServeMux(),
		af:  client,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	s.mux.HandleFunc("GET /api/v1/realtime/runs", s.handleRunsSubscribe)
	return s
}
