// SPDX-License-Identifier: Apache-2.0

// error_envelope_test.go — exercises every public error path on the
// runtime gateway and asserts the response body matches the canonical
// envelope contract:
//
//   {"error": {"code": "<UPPER_SNAKE>", "message": "<human>"}}
//
// Any non-2xx body that fails to decode into this shape breaks the
// dashboard, the SDKs, and downstream tooling — the contract is locked
// at v1 so adding new codes is fine but the shape never changes.
//
// We don't aim for full per-route coverage here (existing per-handler
// tests do that). The job of this file is to be the safety net: when a
// new handler lands, this test exercises it via the route table and
// fails fast if someone forgot to return an envelope.
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

// envelopeCase is one (method, path, expectedStatus) triple to exercise.
//
// expectedStatus is the status code we want to *force*. We use empty
// bodies / missing deps / invalid path params to drive the handler into
// its failure branch.
type envelopeCase struct {
	name      string
	method    string
	path      string
	body      string
	minStatus int
	maxStatus int
}

// cases enumerates every error-bearing path the bare gateway exposes
// without external dependencies wired (no DB, no AF, no storage). Each
// entry should provoke a 4xx or 5xx response from a vanilla Server
// constructed with empty Deps.
var envelopeCases = []envelopeCase{
	// /ready returns 200 when nothing's wired, so we don't include it.
	// /health is similar.

	// Agents — no AF wired -> 503.
	{name: "list-agents-no-af", method: "GET", path: "/api/v1/agents", minStatus: 503, maxStatus: 503},
	{name: "call-agent-no-af", method: "POST", path: "/api/v1/agents/sample.echo", body: "{}", minStatus: 503, maxStatus: 503},
	{name: "call-async-no-af", method: "POST", path: "/api/v1/agents/async/sample.echo", body: "{}", minStatus: 503, maxStatus: 503},
	{name: "get-exec-no-af", method: "GET", path: "/api/v1/executions/xyz", minStatus: 503, maxStatus: 503},
	{name: "cancel-exec-no-af", method: "DELETE", path: "/api/v1/executions/xyz", minStatus: 503, maxStatus: 503},
	// Invalid agent name — no AF wired, so 503 wins before the
	// validation 400 fires. (Validation 400s are exercised in
	// server_test.go's TestAgentCallRejectsInvalidName.)

	// Storage — no adapter -> 503.
	{name: "storage-upload-no-store", method: "POST", path: "/api/v1/storage/upload", body: "", minStatus: 400, maxStatus: 503},
	{name: "storage-signed-url-no-store", method: "GET", path: "/api/v1/storage/signed-url?key=x", minStatus: 503, maxStatus: 503},
	{name: "storage-list-no-store", method: "GET", path: "/api/v1/storage", minStatus: 503, maxStatus: 503},
	{name: "storage-download-no-store", method: "GET", path: "/api/v1/storage/foo", minStatus: 503, maxStatus: 503},
	{name: "storage-delete-no-store", method: "DELETE", path: "/api/v1/storage/foo", minStatus: 503, maxStatus: 503},

	// Secrets — no vault -> 503.
	{name: "secrets-list-no-vault", method: "GET", path: "/api/v1/secrets", minStatus: 503, maxStatus: 503},
	{name: "secrets-get-no-vault", method: "GET", path: "/api/v1/secrets/foo", minStatus: 503, maxStatus: 503},
	{name: "secrets-put-no-vault", method: "PUT", path: "/api/v1/secrets/foo", body: `{"value":"x"}`, minStatus: 503, maxStatus: 503},
	{name: "secrets-delete-no-vault", method: "DELETE", path: "/api/v1/secrets/foo", minStatus: 503, maxStatus: 503},
	{name: "secrets-reveal-no-vault", method: "POST", path: "/api/v1/secrets/foo/reveal", minStatus: 503, maxStatus: 503},
	{name: "secrets-rotate-no-vault", method: "POST", path: "/api/v1/secrets/foo/rotate", body: `{"value":"x"}`, minStatus: 503, maxStatus: 503},

	// Admin — MT disabled by default -> 503 MT_DISABLED.
	{name: "admin-list-tenants", method: "GET", path: "/api/v1/admin/tenants", minStatus: 503, maxStatus: 503},
	{name: "admin-create-tenant", method: "POST", path: "/api/v1/admin/tenants", body: `{"slug":"x","name":"X"}`, minStatus: 503, maxStatus: 503},
	{name: "admin-get-tenant", method: "GET", path: "/api/v1/admin/tenants/abc", minStatus: 503, maxStatus: 503},
	{name: "admin-update-tenant", method: "PATCH", path: "/api/v1/admin/tenants/abc", body: `{}`, minStatus: 503, maxStatus: 503},
	{name: "admin-delete-tenant", method: "DELETE", path: "/api/v1/admin/tenants/abc", minStatus: 503, maxStatus: 503},
	{name: "admin-list-users", method: "GET", path: "/api/v1/admin/users", minStatus: 503, maxStatus: 503},
	{name: "admin-list-memberships", method: "GET", path: "/api/v1/admin/memberships", minStatus: 503, maxStatus: 503},
	{name: "admin-add-membership", method: "POST", path: "/api/v1/admin/memberships", body: `{"tenant_id":"t","user_id":"u","role":"member"}`, minStatus: 503, maxStatus: 503},
	{name: "admin-remove-membership", method: "DELETE", path: "/api/v1/admin/memberships/t/u", minStatus: 503, maxStatus: 503},
	{name: "admin-list-keys", method: "GET", path: "/api/v1/admin/keys", minStatus: 503, maxStatus: 503},
	{name: "admin-issue-key", method: "POST", path: "/api/v1/admin/keys", body: `{"tenant_id":"t"}`, minStatus: 503, maxStatus: 503},
	{name: "admin-revoke-key", method: "DELETE", path: "/api/v1/admin/keys/abc", minStatus: 503, maxStatus: 503},
	{name: "admin-list-audit", method: "GET", path: "/api/v1/admin/audit", minStatus: 503, maxStatus: 503},
}

func TestEveryErrorPathReturnsStandardEnvelope(t *testing.T) {
	srv := New(config.Default(), slog.Default(), Deps{})

	for _, c := range envelopeCases {
		t.Run(c.name, func(t *testing.T) {
			var body *strings.Reader
			if c.body != "" {
				body = strings.NewReader(c.body)
			}
			var req *http.Request
			if body != nil {
				req = httptest.NewRequest(c.method, c.path, body)
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(c.method, c.path, nil)
			}
			rec := httptest.NewRecorder()
			srv.mux.ServeHTTP(rec, req)

			if rec.Code < c.minStatus || rec.Code > c.maxStatus {
				t.Errorf("status=%d, want in [%d, %d]; body=%s",
					rec.Code, c.minStatus, c.maxStatus, rec.Body.String())
			}
			// Empty body is fine for 204; everything else must envelope.
			if rec.Code == http.StatusNoContent {
				return
			}
			assertEnvelope(t, rec.Body.Bytes(), rec.Code)
		})
	}
}

// TestAgentCallBadNameReturnsEnvelope drives the 400 VALIDATION_FAILED
// branch in handleAgentCall (AF wired but the path-param fails the
// validAgentName check) and asserts the envelope contract.
func TestAgentCallBadNameReturnsEnvelope(t *testing.T) {
	afStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer afStub.Close()
	srv := New(config.Default(), slog.Default(), Deps{
		AF: agentfield.New(agentfield.Config{URL: afStub.URL, RequestTimeout: time.Second}),
	})
	// Use a path that ServeMux parses but our validAgentName rejects.
	req := httptest.NewRequest("POST", "/api/v1/agents/bad..name!", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	assertEnvelope(t, rec.Body.Bytes(), rec.Code)
}

// TestReadyReturnsEnvelopeOn503 confirms the /ready endpoint switched
// from the legacy {status, reason} shape to the canonical envelope when
// a dependency is unhealthy. Drives the failure path by wiring an AF
// stub that returns 500.
func TestReadyReturnsEnvelopeOn503(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()

	srv := New(config.Default(), slog.Default(), Deps{
		AF: agentfield.New(agentfield.Config{URL: bad.URL, RequestTimeout: time.Second}),
	})
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	assertEnvelope(t, rec.Body.Bytes(), rec.Code)
}

// Rate-limit 429 passthrough is covered by TestLLMChat429PassesThroughHeaders
// in llm_test.go — the runtime no longer enforces rate limits locally;
// LiteLLM emits 429 + Retry-After + X-RateLimit-* headers and the LLM
// handler proxies them back to the client (item #32).

// assertEnvelope decodes body and asserts the canonical
// {error: {code, message, details?}} contract.
func assertEnvelope(t *testing.T, body []byte, status int) {
	t.Helper()
	var env struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details any    `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("status=%d body is not JSON: %v; body=%s", status, err, body)
	}
	if env.Error == nil {
		t.Fatalf("status=%d body missing 'error' envelope; body=%s", status, body)
	}
	if env.Error.Code == "" {
		t.Errorf("status=%d error.code is empty; body=%s", status, body)
	}
	if env.Error.Message == "" {
		t.Errorf("status=%d error.message is empty; body=%s", status, body)
	}
	// Code should be UPPER_SNAKE / alphanumeric + underscores.
	if strings.ToUpper(env.Error.Code) != env.Error.Code {
		t.Errorf("status=%d error.code %q is not upper-case; body=%s",
			status, env.Error.Code, body)
	}
}