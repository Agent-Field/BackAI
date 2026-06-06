package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
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
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", body["status"])
	}
	checks, _ := body["checks"].(map[string]any)
	if checks == nil {
		t.Fatal("expected checks block in response")
	}
}

func TestReadyReturnsOKWithoutDeps(t *testing.T) {
	s := newBareTestServer(t)
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
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
