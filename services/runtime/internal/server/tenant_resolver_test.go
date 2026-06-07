// SPDX-License-Identifier: Apache-2.0

// Package server — unit tests for the per-request tenant resolver.
//
// These tests don't require Postgres: they exercise the resolver's
// branching logic (multi-tenancy off / API-key path / session path /
// default fallback) by inspecting the request context that the
// resolver writes via tenantctx.
//
// The Postgres-backed paths (bcrypt round-trip on a real key, session-
// to-tenant lookup) are covered by the integration suite in
// internal/tenancy/tenancy_test.go.

package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// newResolverTestServer builds a Server with no DB and no tenancy
// manager. The resolver should still produce a default tenant when
// multi-tenancy is OFF and 401 when ON.
func newResolverTestServer(t *testing.T, mtEnabled bool) *Server {
	t.Helper()
	cfg := config.Default()
	if mtEnabled {
		cfg.Modules.Enabled = map[string]bool{"multi-tenancy": true}
	}
	return New(cfg, slog.Default(), Deps{})
}

// runThroughResolver wraps a no-op terminal handler with tenantResolver
// and returns (1) the response recorder and (2) the tenant context
// captured by the terminal handler. Captured context is empty when the
// resolver short-circuits the chain (4xx).
func runThroughResolver(
	t *testing.T,
	s *Server,
	req *http.Request,
) (*httptest.ResponseRecorder, context.Context) {
	t.Helper()
	var captured context.Context
	terminal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Context()
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	s.tenantResolver(terminal).ServeHTTP(rec, req)
	return rec, captured
}

// TestResolverMultiTenancyOff confirms the default-fallback path: when
// the module flag is off, every request — even paths normally gated —
// gets the seeded default tenant id and proceeds.
func TestResolverMultiTenancyOff(t *testing.T) {
	s := newResolverTestServer(t, false)
	req := httptest.NewRequest("GET", "/api/v1/secrets", nil)
	rec, ctx := runThroughResolver(t, s, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with MT off, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := tenantctx.TenantID(ctx); got != tenancy.DefaultTenantID {
		t.Errorf("tenant id = %q, want default %q", got, tenancy.DefaultTenantID)
	}
	if got := tenantctx.APIKeyID(ctx); got != "" {
		t.Errorf("api key id should be empty in MT-off path, got %q", got)
	}
}

// TestResolverPublicPaths confirms /health, /ready, /metrics, and the
// dashboard read prefixes skip resolution even when MT is on. Without
// this, the dashboard's first load would 401 before the operator logs
// in.
func TestResolverPublicPaths(t *testing.T) {
	s := newResolverTestServer(t, true)
	cases := []string{
		"/health",
		"/ready",
		"/metrics",
		"/openapi.json",
		"/api/v1/agents",
		"/api/v1/runs?limit=10",
		"/api/v1/home/overview",
		"/api/v1/modules",
	}
	for _, path := range cases {
		req := httptest.NewRequest("GET", path, nil)
		rec, ctx := runThroughResolver(t, s, req)
		if rec.Code != http.StatusOK {
			t.Errorf("path %q: expected 200 (public bypass), got %d", path, rec.Code)
		}
		// Public paths are intentionally context-less — the resolver
		// must NOT inject a tenant id (that would mask handlers that
		// forget to check).
		if got := tenantctx.TenantID(ctx); got != "" {
			t.Errorf("path %q: public bypass should not set tenant, got %q", path, got)
		}
	}
}

// TestResolverDeniesUnauthenticatedWhenMTOn proves the failure mode:
// MT on + no credentials -> 401 with the standard envelope.
func TestResolverDeniesUnauthenticatedWhenMTOn(t *testing.T) {
	s := newResolverTestServer(t, true)
	req := httptest.NewRequest("GET", "/api/v1/secrets", nil)
	rec, ctx := runThroughResolver(t, s, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	if tenantctx.TenantID(ctx) != "" {
		t.Error("tenant should not be set on 401 path")
	}
	body := rec.Body.String()
	if !contains(body, "UNAUTHENTICATED") {
		t.Errorf("response should carry UNAUTHENTICATED code, got %s", body)
	}
}

// TestResolverInvalidBearerReturns401 confirms a malformed bearer token
// (e.g. wrong shape) produces 401 with the INVALID_API_KEY code
// regardless of which sentinel parseToken returned. This is the
// existence-oracle defence — all four failure modes collapse to one
// wire response.
func TestResolverInvalidBearerReturns401(t *testing.T) {
	s := newResolverTestServer(t, true)
	req := httptest.NewRequest("GET", "/api/v1/secrets", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec, _ := runThroughResolver(t, s, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if !contains(rec.Body.String(), "INVALID_API_KEY") {
		t.Errorf("body should carry INVALID_API_KEY, got %s", rec.Body.String())
	}
}

// TestResolverIsPublicPath documents the path-matching logic so future
// route additions know exactly which prefix to use.
func TestResolverIsPublicPath(t *testing.T) {
	cases := []struct {
		path   string
		public bool
	}{
		{"/health", true},
		{"/ready", true},
		{"/metrics", true},
		{"/openapi.json", true},
		{"/api/v1/agents", true},
		{"/api/v1/agents/foo", true}, // sub-path matches via prefix
		{"/api/v1/runs", true},
		{"/api/v1/home/overview", true},
		{"/api/v1/cost", true},
		{"/api/v1/modules", true},
		{"/api/v1/queues", true},
		{"/api/v1/admin/tenants", true},   // admin gates itself
		{"/api/v1/db/tables", true},       // DB studio is dashboard-gated
		{"/api/v1/db/sql", true},          // DB studio is dashboard-gated
		{"/api/v1/secrets", false},
		{"/api/v1/storage", false},
		{"/api/v1/jobs", false},
		{"/randompath", false},
	}
	for _, c := range cases {
		got := isPublicPath(c.path)
		if got != c.public {
			t.Errorf("isPublicPath(%q) = %v, want %v", c.path, got, c.public)
		}
	}
}

// contains is a tiny strings.Contains alias — kept local so the test
// file's imports stay minimal.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}