// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/gorilla/websocket"
)

func newBareTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.Default()
	return New(cfg, slog.Default(), Deps{})
}

func TestHealthReturnsOKWithoutDeps(t *testing.T) {
	s := newBareTestServer(t)
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// Phase 14.3: /health is liveness only — status is "alive" and
	// the body carries uptime_s + started_at, NOT a checks block.
	if body["status"] != "alive" {
		t.Errorf("expected status=alive, got %v", body["status"])
	}
	if _, ok := body["uptime_s"]; !ok {
		t.Fatal("expected uptime_s in response")
	}
}

func TestReadyReturns503BeforeMarkReady(t *testing.T) {
	// Phase 14.3: /ready is 503 booting until MarkReady() is called.
	s := newBareTestServer(t)
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 before MarkReady, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "booting" {
		t.Errorf("expected status=booting, got %v", body["status"])
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header on 503 booting")
	}
}

func TestReadyReturnsOKAfterMarkReady(t *testing.T) {
	s := newBareTestServer(t)
	s.MarkReady()
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after MarkReady, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOpenAPIReturnsStub(t *testing.T) {
	s := newBareTestServer(t)
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["openapi"] != "3.1.0" {
		t.Errorf("expected openapi=3.1.0, got %v", body["openapi"])
	}
}

func TestListAgentsWithoutAFReturns503(t *testing.T) {
	s := newBareTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestAgentCallForwardsToAFAndPreservesStatus(t *testing.T) {
	afSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/execute/sample.echo" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Execution-ID", "exec-123")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"echoed":{"x":1}}`))
	}))
	defer afSrv.Close()

	srv := New(config.Default(), slog.Default(), Deps{
		AF: agentfield.New(agentfield.Config{URL: afSrv.URL}),
	})
	req := httptest.NewRequest("POST", "/api/v1/agents/sample.echo",
		strings.NewReader(`{"input":{"x":1}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Execution-ID") != "exec-123" {
		t.Errorf("expected X-Execution-ID passthrough, got %q", rec.Header().Get("X-Execution-ID"))
	}
	if !strings.Contains(rec.Body.String(), "echoed") {
		t.Errorf("expected body passthrough, got %s", rec.Body.String())
	}
}

func TestAgentCallRejectsInvalidName(t *testing.T) {
	srv := New(config.Default(), slog.Default(), Deps{
		AF: agentfield.New(agentfield.Config{URL: "http://example"}),
	})
	req := httptest.NewRequest("POST", "/api/v1/agents/bad..name!", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAgentCallReturns503WithoutAF(t *testing.T) {
	srv := newBareTestServer(t)
	req := httptest.NewRequest("POST", "/api/v1/agents/sample.echo",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestAsyncAgentCallUsesAFAsyncEndpoint(t *testing.T) {
	gotPath := ""
	afSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"execution_id":"exec-async-1"}`))
	}))
	defer afSrv.Close()

	srv := New(config.Default(), slog.Default(), Deps{
		AF: agentfield.New(agentfield.Config{URL: afSrv.URL}),
	})
	req := httptest.NewRequest("POST", "/api/v1/agents/async/sample.echo",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
	if gotPath != "/api/v1/execute/async/sample.echo" {
		t.Errorf("expected async path forward, got %q", gotPath)
	}
}

func TestRunEventsWebSocketRelaysAgentFieldSSE(t *testing.T) {
	afSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflows/runs/run_123/events/stream" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("expected SSE accept header, got %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: execution.started\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"execution_started\",\"execution_id\":\"exec_1\"}\n\n"))
	}))
	defer afSrv.Close()

	srv := New(config.Default(), slog.Default(), Deps{
		AF: agentfield.New(agentfield.Config{URL: afSrv.URL}),
	})
	// The run-event stream is operator-gated (it can stream any tenant's
	// run); authorize so the test exercises the SSE relay behavior.
	withOperator(srv, "owner")
	httpSrv := httptest.NewServer(srv.srv.Handler)
	defer httpSrv.Close()

	u, err := url.Parse(httpSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.Scheme = "ws"
	u.Path = "/api/v1/runs/run_123/events"
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	var msg map[string]any
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read websocket message: %v", err)
	}
	if msg["type"] != "agentfield.run_event" {
		t.Fatalf("unexpected message type: %v", msg)
	}
	if msg["event"] != "execution.started" {
		t.Fatalf("unexpected event: %v", msg)
	}
	data, ok := msg["data"].(map[string]any)
	if !ok || data["execution_id"] != "exec_1" {
		t.Fatalf("unexpected data: %v", msg["data"])
	}
}

func TestRunEventsReturns404WhenAgentFieldStreamMissing(t *testing.T) {
	afSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer afSrv.Close()

	srv := New(config.Default(), slog.Default(), Deps{
		AF: agentfield.New(agentfield.Config{URL: afSrv.URL}),
	})
	withOperator(srv, "owner")
	req := httptest.NewRequest("GET", "/api/v1/runs/run_404/events", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestValidAgentName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"sample.echo", true},
		{"ns.func_v2", true},
		{"a-b.c-d", true},
		{"", false},
		{"missing-dot", true}, // still valid characters; AF will 404
		{"has/slash", false},
		{"has space", false},
		{"a..b", true}, // valid chars; semantic invalid handled by AF
	}
	for _, c := range cases {
		got := validAgentName(c.name)
		if got != c.ok {
			t.Errorf("validAgentName(%q) = %v, want %v", c.name, got, c.ok)
		}
	}
}

func TestListAgentsForwardsToAF(t *testing.T) {
	// AF stub returning two agents
	afSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agents":[{"node_id":"a"},{"node_id":"b"}]}`))
	}))
	defer afSrv.Close()

	cfg := config.Default()
	srv := New(cfg, slog.Default(), Deps{
		AF: agentfield.New(agentfield.Config{URL: afSrv.URL}),
	})
	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	agents, _ := body["agents"].([]any)
	if len(agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(agents))
	}
}
