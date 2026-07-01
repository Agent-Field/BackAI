// SPDX-License-Identifier: Apache-2.0

// agent_error_test.go — A1: unknown routes and upstream agent errors
// must surface the canonical error envelope, not Go's plain-text 404 or
// the agent's raw per-framework error shape.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

// newAgentStubServer builds a Server whose AF client points at a stub
// returning the given status + body for every execute call.
func newAgentStubServer(t *testing.T, status int, contentType, body string) *Server {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.Close)
	return New(config.Default(), slog.Default(), Deps{
		AF: agentfield.New(agentfield.Config{URL: stub.URL, RequestTimeout: 2 * time.Second}),
	})
}

func postAgent(t *testing.T, srv *Server, call string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/agents/"+call, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	return rec
}

// C1: an unregistered path returns a JSON NOT_FOUND envelope, not Go's
// plain-text "404 page not found".
func TestUnknownRouteReturnsJSON404(t *testing.T) {
	srv := New(config.Default(), slog.Default(), Deps{})
	req := httptest.NewRequest("GET", "/api/v1/does-not-exist", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q, want json", ct)
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "NOT_FOUND")
}

// C2: a 404 from the agent maps to AGENT_NOT_FOUND, preserving the status.
func TestAgentUpstream404MapsToEnvelope(t *testing.T) {
	srv := newAgentStubServer(t, http.StatusNotFound, "text/plain", "no such reasoner")
	rec := postAgent(t, srv, "sample.missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "AGENT_NOT_FOUND")
}

// C3: a 500 from the agent with a JSON body maps to AGENT_ERROR and
// preserves the upstream payload under details.upstream.
func TestAgentUpstream500PreservesDetails(t *testing.T) {
	srv := newAgentStubServer(t, http.StatusInternalServerError, "application/json", `{"boom":"kaboom"}`)
	rec := postAgent(t, srv, "sample.explode")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "AGENT_ERROR")

	var parsed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	env := parsed["error"].(map[string]any)
	details, ok := env["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected details object, got %v", env["details"])
	}
	upstream, ok := details["upstream"].(map[string]any)
	if !ok || upstream["boom"] != "kaboom" {
		t.Fatalf("upstream payload not preserved: %v", details)
	}
}

// C4: if the agent already returns a canonical envelope, it is forwarded
// unchanged (no double-wrapping) with its own code preserved.
func TestAgentUpstreamCanonicalEnvelopePassesThrough(t *testing.T) {
	srv := newAgentStubServer(t, http.StatusBadRequest, "application/json",
		`{"error":{"code":"GUARDRAIL_BLOCKED","message":"nope"}}`)
	rec := postAgent(t, srv, "sample.blocked")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	// Code preserved verbatim, not remapped to AGENT_CALL_REJECTED.
	assertErrorEnvelope(t, rec.Body.Bytes(), "GUARDRAIL_BLOCKED")
}

// C5: a successful agent call still passes through untouched.
func TestAgentUpstreamSuccessPassesThrough(t *testing.T) {
	srv := newAgentStubServer(t, http.StatusOK, "application/json", `{"result":"ok"}`)
	rec := postAgent(t, srv, "sample.echo")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var parsed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if parsed["result"] != "ok" {
		t.Fatalf("success body altered: %s", rec.Body.String())
	}
	if _, isErr := parsed["error"]; isErr {
		t.Fatalf("success response should not be an error envelope: %s", rec.Body.String())
	}
}
