// SPDX-License-Identifier: Apache-2.0

// run_agentfield_test.go — coverage for the #25 per-run AgentField proxy
// handlers (GET /agentfield + POST cancel/pause/resume/request-approval).
//
// These tests stand up a fake AgentField HTTP server with httptest and
// point the runtime's agentfield.Client at it. No DB, no real AgentField
// required.

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

// newAFTestServer constructs a Server with its AgentField client aimed
// at the given httptest URL. Used by the run_agentfield handler tests.
func newAFTestServer(afURL string) *Server {
	cfg := config.Default()
	return New(cfg, slog.Default(), Deps{
		AF: agentfield.New(agentfield.Config{URL: afURL}),
	})
}

func TestRunAgentFieldHappyPath(t *testing.T) {
	t.Setenv("AF_STACK_AGENTFIELD_PUBLIC_URL", "https://af.example.com")

	af := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agentic/run/run_1" {
			t.Errorf("AF got unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"execution_id": "exec_1",
			"run_id": "run_1",
			"status": "running",
			"agent_name": "pricing"
		}`))
	}))
	defer af.Close()

	s := newAFTestServer(af.URL)
	req := httptest.NewRequest("GET", "/api/v1/runs/run_1/agentfield", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body runAgentFieldResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Overview.ExecutionID != "exec_1" {
		t.Errorf("execution_id: %q", body.Overview.ExecutionID)
	}
	if body.AgentFieldURL != "https://af.example.com" {
		t.Errorf("agentfield_url: %q", body.AgentFieldURL)
	}
	if !strings.HasSuffix(body.DetailsURL, "/agent-api/executions/exec_1/details") {
		t.Errorf("details_url: %q", body.DetailsURL)
	}
	// running → cancel + pause + request-approval, no resume
	if len(body.ActionsAvailable) != 3 {
		t.Errorf("expected 3 actions for running, got %v", body.ActionsAvailable)
	}
}

func TestRunAgentFieldAFUnreachable(t *testing.T) {
	// Point the AF client at a closed port so the upstream GET fails.
	s := newAFTestServer("http://127.0.0.1:1")
	req := httptest.NewRequest("GET", "/api/v1/runs/run_x/agentfield", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 when AF unreachable, got %d body=%s",
			rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	envelope, _ := body["error"].(map[string]any)
	if envelope["code"] != "AGENTFIELD_UNREACHABLE" {
		t.Errorf("expected AGENTFIELD_UNREACHABLE, got %v", envelope["code"])
	}
}

func TestRunAgentFieldNotConfigured(t *testing.T) {
	// No AF client wired — should 503 cleanly.
	cfg := config.Default()
	s := New(cfg, slog.Default(), Deps{})
	req := httptest.NewRequest("GET", "/api/v1/runs/run_x/agentfield", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestRunCancelProxiesPOST(t *testing.T) {
	var sawCancel atomic.Bool
	af := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/agentic/run/run_1":
			// Overview resolution call.
			_, _ = w.Write([]byte(`{"execution_id":"exec_1","status":"running"}`))
		case r.URL.Path == "/agent-api/executions/exec_1/cancel":
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			sawCancel.Store(true)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected upstream path %q", r.URL.Path)
		}
	}))
	defer af.Close()

	s := newAFTestServer(af.URL)
	req := httptest.NewRequest("POST", "/api/v1/runs/run_1/cancel", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawCancel.Load() {
		t.Error("expected AgentField cancel endpoint to be hit")
	}
}

func TestRunPauseResumeRequestApprovalProxyPOST(t *testing.T) {
	cases := []struct {
		verb     string
		runPath  string
		afSuffix string
	}{
		{"pause", "/api/v1/runs/run_1/pause", "/pause"},
		{"resume", "/api/v1/runs/run_1/resume", "/resume"},
		{"request-approval", "/api/v1/runs/run_1/request-approval", "/request-approval"},
	}
	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			var sawAction atomic.Bool
			af := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/agentic/run/run_1":
					_, _ = w.Write([]byte(`{"execution_id":"exec_1","status":"paused"}`))
				case strings.HasSuffix(r.URL.Path, tc.afSuffix):
					sawAction.Store(true)
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"ok":true}`))
				default:
					t.Errorf("unexpected upstream path %q", r.URL.Path)
				}
			}))
			defer af.Close()

			s := newAFTestServer(af.URL)
			req := httptest.NewRequest("POST", tc.runPath, nil)
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
			}
			if !sawAction.Load() {
				t.Errorf("expected AgentField %s endpoint to be hit", tc.verb)
			}
		})
	}
}

func TestRunCancelGracefulOnAFError(t *testing.T) {
	// AgentField rejects the cancel with 409 → handler should propagate
	// as 502 AGENTFIELD_UNREACHABLE (the agentfield package emits a
	// non-typed error; we map the substring conservatively).
	af := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agentic/run/run_1" {
			_, _ = w.Write([]byte(`{"execution_id":"exec_1","status":"running"}`))
			return
		}
		http.Error(w, "already cancelled", http.StatusConflict)
	}))
	defer af.Close()

	s := newAFTestServer(af.URL)
	req := httptest.NewRequest("POST", "/api/v1/runs/run_1/cancel", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code < 400 {
		t.Fatalf("expected error status, got %d", rec.Code)
	}
}

func TestRunAgentFieldRejectsInvalidRunID(t *testing.T) {
	s := newAFTestServer("http://127.0.0.1:1")
	// Build a request manually because path validation runs before any
	// upstream call.
	req := httptest.NewRequest("GET", "/api/v1/runs/not%20valid/agentfield", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid id, got %d", rec.Code)
	}
}

func TestActionsForStatusMatrix(t *testing.T) {
	cases := []struct {
		status string
		want   int // number of actions expected
	}{
		{"running", 3},
		{"paused", 3},
		{"awaiting_approval", 2},
		{"queued", 1},
		{"succeeded", 0},
		{"failed", 0},
		{"cancelled", 0},
		{"unknown-state", 4},
	}
	for _, tc := range cases {
		got := actionsForStatus(tc.status)
		if len(got) != tc.want {
			t.Errorf("actionsForStatus(%q) = %v (len %d), want %d actions",
				tc.status, got, len(got), tc.want)
		}
	}
}
