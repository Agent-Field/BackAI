// SPDX-License-Identifier: Apache-2.0

// Package server — unit + integration tests for per-route API-key scope
// enforcement (PRD R1) and the /ready RLS-safety gate.
//
// These tests need no Postgres: the scope logic is pure, and the
// end-to-end resolver path uses an injected keyVerifier fake (the same
// seam pattern as operatorResolver) so a bearer token resolves to a key
// with exactly the scopes under test.

package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
)

// ─── Pure scope-satisfaction logic ────────────────────────────────────────

func TestScopeSatisfied(t *testing.T) {
	cases := []struct {
		name     string
		held     []string
		required string
		want     bool
	}{
		// Legacy / full-access grants pass everything.
		{"empty scopes = full access", nil, "storage:write", true},
		{"wildcard passes", []string{"*"}, "storage:write", true},
		{"admin grant passes", []string{"admin"}, "secrets:write", true},
		{"no requirement always passes", []string{"storage:read"}, "", true},

		// Narrow keys: exact + write-implies-read.
		{"exact read", []string{"storage:read"}, "storage:read", true},
		{"write implies read", []string{"storage:write"}, "storage:read", true},
		{"read does NOT imply write", []string{"storage:read"}, "storage:write", false},
		{"bare area grants any action", []string{"storage"}, "storage:write", true},
		{"area wildcard grants any action", []string{"storage:*"}, "storage:write", true},

		// Cross-area denials — the fail-closed cases from the acceptance.
		{"storage:read denied llm:write", []string{"storage:read"}, "llm:write", false},
		{"storage:read denied secrets:read", []string{"storage:read"}, "secrets:read", false},
		{"storage:read denied admin:write", []string{"storage:read"}, "admin:write", false},
		{"storage:read denied storage:write", []string{"storage:read"}, "storage:write", false},

		// Multiple held scopes: any one may satisfy.
		{"multi held picks match", []string{"llm:read", "storage:write"}, "storage:read", true},
		{"multi held no match", []string{"llm:read", "jobs:read"}, "storage:write", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scopeSatisfied(tc.held, tc.required); got != tc.want {
				t.Fatalf("scopeSatisfied(%v, %q) = %v, want %v", tc.held, tc.required, got, tc.want)
			}
		})
	}
}

func TestRequiredScopeFor(t *testing.T) {
	cases := []struct {
		method, path string
		want         string
	}{
		{"GET", "/api/v1/storage", "storage:read"},
		{"GET", "/api/v1/storage/signed-url", "storage:read"},
		{"GET", "/api/v1/storage/a/b.txt", "storage:read"},
		{"POST", "/api/v1/storage/upload", "storage:write"},
		{"DELETE", "/api/v1/storage/a/b.txt", "storage:write"},
		{"POST", "/api/v1/llm/chat/completions", "llm:write"},
		{"POST", "/api/v1/embeddings", "llm:write"},
		{"GET", "/api/v1/llm/models", "llm:read"},
		{"GET", "/api/v1/llm/cache/stats", ""}, // operator-only, no tenant scope
		{"POST", "/api/v1/agents/supportdesk.echo", "agents:write"},
		{"GET", "/api/v1/executions/exec-1", "agents:read"},
		{"DELETE", "/api/v1/executions/exec-1", "agents:write"},
		{"POST", "/api/v1/jobs", "jobs:write"},
		{"GET", "/api/v1/jobs", "jobs:read"},
		{"POST", "/api/v1/search", "search:read"}, // query is a read
		{"PUT", "/api/v1/search/documents", "search:write"},
		{"GET", "/api/v1/activity", "activity:read"},
		{"POST", "/api/v1/activity", "activity:write"},
		{"GET", "/api/v1/memory/get", "memory:read"},
		{"POST", "/api/v1/memory/search", "memory:read"},
		{"PUT", "/api/v1/memory", "memory:write"},
		{"POST", "/api/v1/webhooks/emit", "webhooks:write"},
		{"GET", "/api/v1/webhooks/subscriptions", "webhooks:read"},
		{"PUT", "/api/v1/secrets/openai", "secrets:write"},
		{"GET", "/api/v1/admin/tenants", "admin:read"},
		{"POST", "/api/v1/admin/keys", "admin:write"},
		// Cross-stream families wired at integration time.
		{"POST", "/api/v1/jobs/worker/lease", "jobs:work"},
		{"POST", "/api/v1/jobs/worker/heartbeat", "jobs:work"},
		{"GET", "/api/v1/vault/secrets", "secrets:read"},
		{"PUT", "/api/v1/vault/secrets/stripe", "secrets:write"},
		{"POST", "/api/v1/vault/secrets/stripe/reveal", "secrets:write"},
		{"GET", "/api/v1/connections", "connections:read"},
		{"POST", "/api/v1/connections/c-1/request", "connections:write"},
		{"GET", "/api/v1/workload/notes/notes", "workload:read"},
		{"POST", "/api/v1/workload/notes/notes", "workload:write"},
		// Unmapped / public routes carry no scope requirement.
		{"GET", "/api/v1/agents", ""},
		{"GET", "/health", ""},
		{"GET", "/api/v1/home/overview", ""},
	}
	for _, tc := range cases {
		if got := requiredScopeFor(tc.method, tc.path); got != tc.want {
			t.Errorf("requiredScopeFor(%q, %q) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

// ─── End-to-end resolver enforcement ──────────────────────────────────────

// newScopedKeyServer builds an MT-on server whose bearer verification is
// faked to return a key carrying exactly `scopes`.
func newScopedKeyServer(t *testing.T, scopes []string) *Server {
	t.Helper()
	cfg := config.Default()
	cfg.Modules.Enabled = map[string]bool{"multi-tenancy": true}
	s := New(cfg, slog.New(slog.NewTextHandler(discard{}, nil)), Deps{})
	s.keyVerifier = func(_ context.Context, _ string) (tenancy.APIKey, error) {
		return tenancy.APIKey{ID: "key-1", TenantID: "tenant-a", Scopes: scopes}, nil
	}
	return s
}

// runScoped drives one request through the resolver with a bearer token,
// terminating in a 200 handler. A denied scope short-circuits with 403
// before the terminal handler runs.
func runScoped(t *testing.T, s *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	terminal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	s.tenantResolver(terminal).ServeHTTP(rec, req)
	return rec
}

// TestScopeEnforcementNarrowKey is the core acceptance: a key scoped
// [storage:read] reads storage (200) but is fail-closed (403 SCOPE_DENIED)
// on storage writes and on other families it lacks the scope for.
func TestScopeEnforcementNarrowKey(t *testing.T) {
	s := newScopedKeyServer(t, []string{"storage:read"})
	cases := []struct {
		method, path string
		wantCode     int
	}{
		{"GET", "/api/v1/storage", http.StatusOK},
		{"GET", "/api/v1/storage/report.txt", http.StatusOK},
		{"POST", "/api/v1/storage/upload", http.StatusForbidden},
		{"DELETE", "/api/v1/storage/report.txt", http.StatusForbidden},
		{"POST", "/api/v1/llm/chat/completions", http.StatusForbidden},
		{"POST", "/api/v1/jobs", http.StatusForbidden},
		{"POST", "/api/v1/agents/supportdesk.echo", http.StatusForbidden},
	}
	for _, tc := range cases {
		rec := runScoped(t, s, tc.method, tc.path)
		if rec.Code != tc.wantCode {
			t.Errorf("%s %s: code = %d, want %d (body=%s)", tc.method, tc.path, rec.Code, tc.wantCode, rec.Body.String())
			continue
		}
		if tc.wantCode == http.StatusForbidden {
			if !contains(rec.Body.String(), "SCOPE_DENIED") {
				t.Errorf("%s %s: body should carry SCOPE_DENIED, got %s", tc.method, tc.path, rec.Body.String())
			}
		}
	}
}

// TestScopeDeniedNamesMissingScope proves the 403 envelope names the exact
// scope the key lacks — so callers can self-remediate.
func TestScopeDeniedNamesMissingScope(t *testing.T) {
	s := newScopedKeyServer(t, []string{"storage:read"})
	rec := runScoped(t, s, "POST", "/api/v1/storage/upload")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if !contains(rec.Body.String(), "storage:write") {
		t.Errorf("403 body should name the missing scope storage:write, got %s", rec.Body.String())
	}
}

// TestScopeWildcardAndEmptyPassEverywhere: a ["*"] key and an empty-scope
// (legacy) key both have full tenant access — no route is scope-gated.
func TestScopeWildcardAndEmptyPassEverywhere(t *testing.T) {
	for _, scopes := range [][]string{{"*"}, nil, {}} {
		s := newScopedKeyServer(t, scopes)
		for _, tc := range []struct{ method, path string }{
			{"POST", "/api/v1/storage/upload"},
			{"POST", "/api/v1/llm/chat/completions"},
			{"POST", "/api/v1/agents/supportdesk.echo"},
			{"DELETE", "/api/v1/storage/report.txt"},
		} {
			rec := runScoped(t, s, tc.method, tc.path)
			if rec.Code != http.StatusOK {
				t.Errorf("scopes=%v %s %s: code = %d, want 200 (full access) body=%s",
					scopes, tc.method, tc.path, rec.Code, rec.Body.String())
			}
		}
	}
}

// TestScopeWriteImpliesRead: a [storage:write] key can also read storage.
func TestScopeWriteImpliesRead(t *testing.T) {
	s := newScopedKeyServer(t, []string{"storage:write"})
	if rec := runScoped(t, s, "GET", "/api/v1/storage"); rec.Code != http.StatusOK {
		t.Errorf("storage:write should satisfy storage:read, got %d", rec.Code)
	}
	if rec := runScoped(t, s, "POST", "/api/v1/storage/upload"); rec.Code != http.StatusOK {
		t.Errorf("storage:write should satisfy storage:write, got %d", rec.Code)
	}
}

// ─── /ready RLS-safety gate ───────────────────────────────────────────────

// TestReadyRLSUnsafe: when the runtime intends to isolate tenants but the
// serving role can bypass RLS (Deps.RLSUnsafe), /ready reports 503
// "rls_unsafe" even though the process is otherwise up.
func TestReadyRLSUnsafe(t *testing.T) {
	s := New(config.Default(), slog.New(slog.NewTextHandler(discard{}, nil)), Deps{RLSUnsafe: true})
	s.MarkReady()
	rec := httptest.NewRecorder()
	s.handleReady(rec, httptest.NewRequest("GET", "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with RLS unsafe, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "rls_unsafe") {
		t.Errorf("body should carry rls_unsafe status, got %s", rec.Body.String())
	}
}

// TestReadyRLSSafe: with RLSUnsafe false and no DB, /ready is 200 once
// booted — the gate must not fire for the common (safe) case.
func TestReadyRLSSafe(t *testing.T) {
	s := New(config.Default(), slog.New(slog.NewTextHandler(discard{}, nil)), Deps{})
	s.MarkReady()
	rec := httptest.NewRecorder()
	s.handleReady(rec, httptest.NewRequest("GET", "/ready", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 ready, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ─── OpenAPI security annotation ──────────────────────────────────────────

// TestOpenAPIScopeAnnotation confirms the spec carries x-required-scope +
// x-principals on a scoped route, sourced from the same registry the
// resolver enforces.
func TestOpenAPIScopeAnnotation(t *testing.T) {
	s := New(config.Default(), slog.New(slog.NewTextHandler(discard{}, nil)), Deps{})
	spec := s.openapi.Build()
	op, ok := spec.Paths["/api/v1/storage/upload"].Operations["post"]
	if !ok {
		t.Fatal("expected POST /api/v1/storage/upload in spec")
	}
	if op.RequiredScope != "storage:write" {
		t.Errorf("x-required-scope = %q, want storage:write", op.RequiredScope)
	}
	if len(op.Principals) == 0 {
		t.Error("expected x-principals to be populated")
	}
}

// discard is an io.Writer sink for the test logger (avoids importing io
// solely for io.Discard alongside the rest of the file's imports).
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
