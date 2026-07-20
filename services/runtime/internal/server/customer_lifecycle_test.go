// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantrole"
)

func lifecycleServer(t *testing.T, mode string) *Server {
	t.Helper()
	cfg := config.Default()
	if mode != "" {
		cfg.Mode = mode
	}
	return New(cfg, slog.Default(), Deps{})
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v (%s)", err, rec.Body.String())
	}
	return env.Error.Code
}

// Contract: without a resolved tenant, every /me/* handler is 401.
func TestRequireTenantCap_TenantRequired(t *testing.T) {
	s := lifecycleServer(t, config.ModeSaaS)
	req := httptest.NewRequest("GET", "/api/v1/me/keys", nil)
	rec := httptest.NewRecorder()
	_, _, ok := s.requireTenantCap(rec, req, tenantrole.CapKeysRead)
	if ok {
		t.Fatal("expected ok=false with no tenant")
	}
	if rec.Code != http.StatusUnauthorized || errCode(t, rec) != "TENANT_REQUIRED" {
		t.Errorf("got %d/%s, want 401/TENANT_REQUIRED", rec.Code, errCode(t, rec))
	}
}

// Contract: an API-key principal (tenant bound, no user) can't be attributed
// a membership role, so tenant-management routes demand a session.
func TestRequireTenantCap_SessionRequired(t *testing.T) {
	s := lifecycleServer(t, config.ModeSaaS)
	ctx := tenantctx.WithTenant(t.Context(), "11111111-1111-1111-1111-111111111111", "key-1")
	req := httptest.NewRequest("POST", "/api/v1/me/keys", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	_, _, ok := s.requireTenantCap(rec, req, tenantrole.CapKeysManage)
	if ok {
		t.Fatal("expected ok=false for an API-key (no user) principal")
	}
	if rec.Code != http.StatusUnauthorized || errCode(t, rec) != "SESSION_REQUIRED" {
		t.Errorf("got %d/%s, want 401/SESSION_REQUIRED", rec.Code, errCode(t, rec))
	}
}

// Contract: personal mode is a single-user app — the sole user owns the
// default tenant and every capability is granted with no RBAC/DB lookup.
func TestRequireTenantCap_PersonalModeGrantsAll(t *testing.T) {
	s := lifecycleServer(t, config.ModePersonal)
	ctx := tenantctx.WithTenantAndUser(t.Context(), "00000000-0000-0000-0000-000000000000", "", "")
	req := httptest.NewRequest("POST", "/api/v1/me/keys", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	for _, cap := range []tenantrole.Capability{
		tenantrole.CapKeysManage, tenantrole.CapTenantManage, tenantrole.CapBillingManage,
	} {
		if _, _, ok := s.requireTenantCap(rec, req, cap); !ok {
			t.Errorf("personal mode should grant %q", cap)
		}
	}
}

// Contract: the token-based accept route bypasses the tenant resolver (the
// invitee has no membership yet), while /api/v1/me/* routes go THROUGH it so
// tenant + user are bound for RBAC.
func TestAcceptRoute_ResolverClassification(t *testing.T) {
	if !isPublicPath("/api/v1/invitations/accept") {
		t.Error("accept route must bypass the resolver (public prefix)")
	}
	for _, p := range []string{"/api/v1/me/keys", "/api/v1/me/invitations", "/api/v1/me/tenant"} {
		if isPublicPath(p) {
			t.Errorf("%s must go THROUGH the resolver (not public)", p)
		}
	}
}
