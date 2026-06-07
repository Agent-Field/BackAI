// SPDX-License-Identifier: Apache-2.0

// Package server — secrets handler tests.
//
// These tests cover the "no vault wired" path: every secrets endpoint
// must return 503 with a clean error envelope so a runtime without DB
// (or without a KEK) doesn't crash the dashboard. They also pin the
// JSON shape that handlers emit (snake_case, nullable fields rendered
// as null, etc.) without requiring Postgres.
//
// DB-backed round-trips (PUT + reveal returns the same value) are
// exercised by the secrets package unit tests + a future integration
// test under build tag `integration`.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newSecretsTestServer wires a Server with no DB and no vault. Every
// /api/v1/secrets/* handler should respond with 503.
func newSecretsTestServer(t *testing.T) *Server {
	t.Helper()
	return newBareTestServer(t)
}

func TestListSecretsReturns503WhenVaultMissing(t *testing.T) {
	s := newSecretsTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/secrets", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "SECRETS_NOT_CONFIGURED")
}

func TestGetSecretMetadataReturns503WhenVaultMissing(t *testing.T) {
	s := newSecretsTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/secrets/openai_api_key", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "SECRETS_NOT_CONFIGURED")
}

func TestRevealSecretReturns503WhenVaultMissing(t *testing.T) {
	s := newSecretsTestServer(t)
	req := httptest.NewRequest("POST", "/api/v1/secrets/openai_api_key/reveal", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "SECRETS_NOT_CONFIGURED")
}

func TestPutSecretReturns503WhenVaultMissing(t *testing.T) {
	s := newSecretsTestServer(t)
	req := httptest.NewRequest("PUT", "/api/v1/secrets/k",
		strings.NewReader(`{"value":"v"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "SECRETS_NOT_CONFIGURED")
}

func TestDeleteSecretReturns503WhenVaultMissing(t *testing.T) {
	s := newSecretsTestServer(t)
	req := httptest.NewRequest("DELETE", "/api/v1/secrets/k", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "SECRETS_NOT_CONFIGURED")
}

func TestRotateSecretReturns503WhenVaultMissing(t *testing.T) {
	s := newSecretsTestServer(t)
	req := httptest.NewRequest("POST", "/api/v1/secrets/k/rotate",
		strings.NewReader(`{"value":"new"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "SECRETS_NOT_CONFIGURED")
}

// assertErrorEnvelope confirms the body matches the canonical
// {"error": {"code": "...", "message": "..."}} shape.
func assertErrorEnvelope(t *testing.T, body []byte, wantCode string) {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("body is not JSON: %v / %s", err, body)
	}
	envelope, ok := parsed["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got %s", body)
	}
	if code, _ := envelope["code"].(string); code != wantCode {
		t.Errorf("error code = %q, want %q", code, wantCode)
	}
	if msg, _ := envelope["message"].(string); msg == "" {
		t.Errorf("error message empty")
	}
}

// TestDefaultTenantConstant pins the seeded tenant UUID so dashboard
// devs can rely on it. If this ever moves, every multi-tenant flow
// elsewhere needs review.
func TestDefaultTenantConstant(t *testing.T) {
	if defaultTenantUUID != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("defaultTenantUUID drifted: %q", defaultTenantUUID)
	}
}

// TestParseRFC3339 covers both nano and non-nano flavours.
func TestParseRFC3339(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"2026-06-06T12:00:00Z", true},
		{"2026-06-06T12:00:00.123456789Z", true},
		{"2026-06-06T12:00:00+02:00", true},
		{"not a timestamp", false},
		{"", false},
	}
	for _, c := range cases {
		_, err := parseRFC3339(c.in)
		if (err == nil) != c.ok {
			t.Errorf("parseRFC3339(%q) ok=%v err=%v", c.in, err == nil, err)
		}
	}
}