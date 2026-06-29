// SPDX-License-Identifier: Apache-2.0

// auth.go — identity introspection for the SDK's suite.auth namespace.
//
// Auth lifecycle (sign-up, sign-in, password reset) is owned by the
// app-layer (better-auth in the customer-app). The runtime authenticates a
// request by API key or session and resolves it to a tenant/user via the
// tenant_resolver middleware. This endpoint simply reflects that resolved
// identity back to the caller — the read-only "who am I" that app code needs
// to confirm which tenant a credential maps to.

package server

import (
	"net/http"

	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// whoamiResponse mirrors the SDK's WhoAmI shape (suite.auth.whoami()).
type whoamiResponse struct {
	Authenticated bool   `json:"authenticated"`
	TenantID      string `json:"tenant_id"`
	UserID        string `json:"user_id"`
	APIKeyID      string `json:"api_key_id"`
}

func (s *Server) registerAuthRoutes() {
	s.mux.HandleFunc("GET /api/v1/auth/whoami", s.handleAuthWhoami)
}

func (s *Server) registerAuthOpenAPI() {
	if s.openapi == nil {
		return
	}
	s.openapi.AddTag("auth", "Identity introspection for the current credential")
	s.openapi.Register("GET", "/api/v1/auth/whoami", openapi.RouteMeta{
		Summary: "Resolve the tenant/user/key the current credential maps to",
		Tags:    []string{"auth"},
	})
}

// handleAuthWhoami reports the identity the current request resolves to. It
// never errors: an unauthenticated caller simply gets authenticated=false
// with empty fields.
func (s *Server) handleAuthWhoami(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := tenantctx.TenantID(ctx)
	writeJSON(w, http.StatusOK, whoamiResponse{
		Authenticated: tenantID != "",
		TenantID:      tenantID,
		UserID:        tenantctx.UserID(ctx),
		APIKeyID:      tenantctx.APIKeyID(ctx),
	})
}
