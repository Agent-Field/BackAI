// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// Contract: whoami reflects the identity the current credential resolves to.
func TestAuthWhoami_Authenticated(t *testing.T) {
	srv := New(config.Default(), testLogger(), Deps{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	req = req.WithContext(
		tenantctx.WithTenantAndUser(req.Context(), "tenant-1", "key-1", "user-1"),
	)
	rr := httptest.NewRecorder()
	srv.handleAuthWhoami(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got whoamiResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Authenticated || got.TenantID != "tenant-1" || got.UserID != "user-1" || got.APIKeyID != "key-1" {
		t.Fatalf("unexpected whoami: %+v", got)
	}
}

// Contract: an unauthenticated caller gets authenticated=false, not an error.
func TestAuthWhoami_Unauthenticated(t *testing.T) {
	srv := New(config.Default(), testLogger(), Deps{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	rr := httptest.NewRecorder()
	srv.handleAuthWhoami(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var got whoamiResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Authenticated || got.TenantID != "" {
		t.Fatalf("expected unauthenticated empty identity, got %+v", got)
	}
}
