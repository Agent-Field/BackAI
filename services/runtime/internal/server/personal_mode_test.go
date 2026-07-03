// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

func newModeServer(cfg config.Config) *Server {
	return &Server{
		mux: http.NewServeMux(),
		cfg: cfg,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// Personal mode forces auth off: multi-tenancy is reported disabled and the
// operator RBAC gate is bypassed — even if the module flags say otherwise.
func TestPersonalModeForcesAuthOff(t *testing.T) {
	cfg := config.Default()
	cfg.Mode = config.ModePersonal
	// Explicitly turn MT on to prove personal mode overrides it.
	cfg.Modules.Enabled = map[string]bool{"multi-tenancy": true}
	s := newModeServer(cfg)

	if !s.personalMode() {
		t.Fatal("personalMode() = false, want true")
	}
	if s.multiTenancyEnabled() {
		t.Error("multiTenancyEnabled() = true in personal mode, want false (auth off)")
	}
	// operatorAccessDenied must NOT deny (bypassed) in personal mode.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/anything", nil)
	if denied := s.operatorAccessDenied(rec, req, "runs", "read"); denied {
		t.Error("operatorAccessDenied denied a request in personal mode, want allowed")
	}
}

// Personal mode forces billing off regardless of the billing module flag.
func TestPersonalModeForcesBillingOff(t *testing.T) {
	cfg := config.Default()
	cfg.Mode = config.ModePersonal
	cfg.Modules.Enabled = map[string]bool{"billing": true}
	s := newModeServer(cfg)

	if s.billingEnabled() {
		t.Error("billingEnabled() = true in personal mode, want false")
	}
}

// In SaaS mode billing is on by default and multi-tenancy honors its flag.
func TestSaaSModeDefaults(t *testing.T) {
	cfg := config.Default() // Mode defaults to saas
	s := newModeServer(cfg)

	if s.personalMode() {
		t.Error("personalMode() = true for default config, want false")
	}
	if !s.billingEnabled() {
		t.Error("billingEnabled() = false by default in saas, want true")
	}
	if s.multiTenancyEnabled() {
		t.Error("multiTenancyEnabled() = true, want false (MT defaults off)")
	}
}

// GET /api/v1/modules reports mode + billing_enabled, and the multi-tenancy /
// billing module entries reflect the EFFECTIVE (forced-off) state.
func TestModulesStateReportsMode(t *testing.T) {
	cfg := config.Default()
	cfg.Mode = config.ModePersonal
	cfg.Modules.Enabled = map[string]bool{"multi-tenancy": true, "billing": true}
	s := newModeServer(cfg)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/modules", nil)
	s.handleModulesState(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp modulesStateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Mode != config.ModePersonal {
		t.Errorf("mode = %q, want personal", resp.Mode)
	}
	if resp.BillingEnabled {
		t.Error("billing_enabled = true, want false in personal mode")
	}
	if resp.MultiTenancyEnabled {
		t.Error("multi_tenancy_enabled = true, want false in personal mode")
	}
	// The per-module entries must match the effective state, not the raw flags.
	for _, m := range resp.Modules {
		switch m.ID {
		case "multi-tenancy", "billing":
			if m.Enabled {
				t.Errorf("module %q reported enabled, want disabled in personal mode", m.ID)
			}
		}
	}
}

// A saas runtime still reports mode=saas and billing on by default.
func TestModulesStateSaaS(t *testing.T) {
	s := newModeServer(config.Default())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/modules", nil)
	s.handleModulesState(rec, req)

	var resp modulesStateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Mode != config.ModeSaaS {
		t.Errorf("mode = %q, want saas", resp.Mode)
	}
	if !resp.BillingEnabled {
		t.Error("billing_enabled = false, want true in saas")
	}
}
