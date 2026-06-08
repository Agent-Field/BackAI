// SPDX-License-Identifier: Apache-2.0

package agentfield

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("expected /health, got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(HealthStatus{Status: "ok", Version: "1.0.0"})
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, RequestTimeout: time.Second})
	hs, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if hs.Status != "ok" {
		t.Errorf("expected status=ok, got %q", hs.Status)
	}
}

func TestHealthFailsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL})
	if _, err := c.Health(context.Background()); err == nil {
		t.Error("expected error on 500")
	}
}

func TestHealthFailsOnUnreachable(t *testing.T) {
	c := New(Config{URL: "http://127.0.0.1:1", RequestTimeout: 200 * time.Millisecond})
	if _, err := c.Health(context.Background()); err == nil {
		t.Error("expected error when AF unreachable")
	}
}

func TestDiscoverReturnsEmptyOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL})
	agents, err := c.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected empty, got %v", agents)
	}
}

func TestNewURLNormalisation(t *testing.T) {
	c := New(Config{URL: "http://example.com/"})
	if c.BaseURL() != "http://example.com" {
		t.Errorf("expected trailing slash trimmed, got %q", c.BaseURL())
	}
}

func TestStreamRunEventsUsesCurrentRunPath(t *testing.T) {
	gotPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("expected SSE accept header, got %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"execution_started\"}\n\n"))
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL})
	resp, err := c.StreamRunEvents(context.Background(), "run_123")
	if err != nil {
		t.Fatalf("StreamRunEvents: %v", err)
	}
	defer resp.Body.Close()
	if gotPath != "/api/v1/workflows/runs/run_123/events/stream" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) == "" {
		t.Fatal("expected stream body")
	}
}

func TestStreamRunEventsFallsBackToLegacyWorkflowPath(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if len(paths) == 1 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"execution_completed\"}\n\n"))
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL})
	resp, err := c.StreamRunEvents(context.Background(), "wf_123")
	if err != nil {
		t.Fatalf("StreamRunEvents: %v", err)
	}
	defer resp.Body.Close()
	if len(paths) != 2 {
		t.Fatalf("expected two attempts, got %v", paths)
	}
	if paths[1] != "/api/v1/workflows/wf_123/events" {
		t.Fatalf("unexpected fallback path: %q", paths[1])
	}
}

func TestEmptyURLRejected(t *testing.T) {
	c := New(Config{})
	if _, err := c.Health(context.Background()); err == nil {
		t.Error("expected error for empty URL")
	}
	if _, err := c.Discover(context.Background()); err == nil {
		t.Error("expected error for empty URL")
	}
	if _, err := c.StreamRunEvents(context.Background(), "run_1"); err == nil {
		t.Error("expected error for empty URL")
	}
}

// ─── #25 AgentField data in dashboard — new method coverage ───────────────

func TestGetRunOverviewHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agentic/run/run_42" {
			t.Errorf("expected /agentic/run/run_42, got %q", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("missing Accept header, got %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"execution_id": "exec_42",
			"run_id": "run_42",
			"status": "running",
			"agent_name": "pricing",
			"reasoner": "estimate",
			"started_at": "2026-06-07T10:00:00Z",
			"cost_usd": 0.0123,
			"approval_status": "auto"
		}`))
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, RequestTimeout: time.Second})
	overview, err := c.GetRunOverview(context.Background(), "run_42")
	if err != nil {
		t.Fatalf("GetRunOverview: %v", err)
	}
	if overview.ExecutionID != "exec_42" {
		t.Errorf("execution_id: %q", overview.ExecutionID)
	}
	if overview.Status != "running" {
		t.Errorf("status: %q", overview.Status)
	}
	if overview.AgentName != "pricing" {
		t.Errorf("agent_name: %q", overview.AgentName)
	}
	if overview.ApprovalStatus != "auto" {
		t.Errorf("approval_status: %q", overview.ApprovalStatus)
	}
	if len(overview.Extra) == 0 {
		t.Error("expected raw payload in Extra")
	}
}

func TestGetRunOverviewNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, RequestTimeout: time.Second})
	_, err := c.GetRunOverview(context.Background(), "run_404")
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestGetRunOverviewTolerantOnPartialBody(t *testing.T) {
	// AgentField's wire shape evolves — verify a minimal response still
	// produces a usable RunOverview (no decode failure).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status": "queued"}`))
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, RequestTimeout: time.Second})
	overview, err := c.GetRunOverview(context.Background(), "run_min")
	if err != nil {
		t.Fatalf("GetRunOverview: %v", err)
	}
	if overview.RunID != "run_min" {
		t.Errorf("expected run_id back-filled from path, got %q", overview.RunID)
	}
	if overview.Status != "queued" {
		t.Errorf("expected status preserved, got %q", overview.Status)
	}
}

func TestGetExecutionDetailsHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent-api/executions/exec_1/details" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"execution_id":"exec_1","spans":[{"id":"s1"}]}`))
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, RequestTimeout: time.Second})
	body, err := c.GetExecutionDetails(context.Background(), "exec_1")
	if err != nil {
		t.Fatalf("GetExecutionDetails: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("expected non-empty body")
	}
}

func TestCancelAgentExecutionPostsToCorrectPath(t *testing.T) {
	cases := []struct {
		name string
		path string
		fn   func(*Client, context.Context, string) error
	}{
		{"cancel", "/agent-api/executions/exec_x/cancel", (*Client).CancelAgentExecution},
		{"pause", "/agent-api/executions/exec_x/pause", (*Client).PauseAgentExecution},
		{"resume", "/agent-api/executions/exec_x/resume", (*Client).ResumeAgentExecution},
		{"request-approval", "/agent-api/executions/exec_x/request-approval", (*Client).RequestAgentApproval},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotMethod string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotMethod = r.Method
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			}))
			defer srv.Close()

			c := New(Config{URL: srv.URL, RequestTimeout: time.Second})
			if err := tc.fn(c, context.Background(), "exec_x"); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if gotMethod != http.MethodPost {
				t.Errorf("expected POST, got %s", gotMethod)
			}
			if gotPath != tc.path {
				t.Errorf("expected path %q, got %q", tc.path, gotPath)
			}
		})
	}
}

func TestAgentActionPropagatesAgentFieldError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad state", http.StatusConflict)
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, RequestTimeout: time.Second})
	err := c.CancelAgentExecution(context.Background(), "exec_x")
	if err == nil {
		t.Fatal("expected error on 409")
	}
}

func TestAgentFieldURLPrefersPublicEnv(t *testing.T) {
	t.Setenv("AF_STACK_AGENTFIELD_PUBLIC_URL", "https://af.example.com/")
	c := New(Config{URL: "http://internal:8081"})
	if got := c.AgentFieldURL(); got != "https://af.example.com" {
		t.Errorf("expected public env URL with trailing slash trimmed, got %q", got)
	}
}

func TestAgentFieldURLFallsBackToBaseURL(t *testing.T) {
	t.Setenv("AF_STACK_AGENTFIELD_PUBLIC_URL", "")
	c := New(Config{URL: "http://internal:8081"})
	if got := c.AgentFieldURL(); got != "http://internal:8081" {
		t.Errorf("expected base URL fallback, got %q", got)
	}
}

func TestNewAgentAPIMethodsRejectEmptyURL(t *testing.T) {
	c := New(Config{})
	if _, err := c.GetRunOverview(context.Background(), "r"); err == nil {
		t.Error("GetRunOverview should reject empty URL")
	}
	if _, err := c.GetExecutionDetails(context.Background(), "e"); err == nil {
		t.Error("GetExecutionDetails should reject empty URL")
	}
	if err := c.CancelAgentExecution(context.Background(), "e"); err == nil {
		t.Error("CancelAgentExecution should reject empty URL")
	}
}
