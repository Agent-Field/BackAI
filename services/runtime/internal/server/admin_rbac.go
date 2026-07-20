// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
)

type operatorPrincipal struct {
	UserID string
	Email  string
	Role   string
}

var errOperatorNotAllowed = errors.New("server: session user is not an operator")

// adminAccessDenied runs the common admin gate:
//  1. module/store availability
//  2. better-auth operator session lookup
//  3. Casbin role/action/resource policy
func (s *Server) adminAccessDenied(
	w http.ResponseWriter,
	r *http.Request,
	resource string,
	action string,
) bool {
	if s.adminUnavailable(w) {
		return true
	}
	return s.operatorAccessDenied(w, r, resource, action)
}

// operatorPlaneScopeDenied returns true (and writes 403) when a key-issue
// request tries to grant an operator-plane scope ("operator",
// "operator:owner") but the requesting principal is not an owner. Minting
// operator keys is an owner-only power: without this gate an admin-role
// operator (which can write admin:keys but cannot delete) could issue
// itself an operator:owner key and escalate to full owner.
func (s *Server) operatorPlaneScopeDenied(w http.ResponseWriter, r *http.Request, scopes []string) bool {
	grantsOperator := false
	for _, sc := range scopes {
		if strings.HasPrefix(strings.TrimSpace(sc), "operator") {
			grantsOperator = true
			break
		}
	}
	if !grantsOperator || s.personalMode() {
		return false
	}
	resolve := s.operatorResolver
	if resolve == nil {
		resolve = s.resolveOperatorPrincipal
	}
	principal, err := resolve(r.Context(), r)
	if err != nil || principal.Role != rbac.RoleOwner {
		writeJSON(w, http.StatusForbidden,
			errEnvelope("RBAC_DENIED", "only an owner may grant operator-plane scopes"))
		return true
	}
	return false
}

func (s *Server) operatorAccessDenied(
	w http.ResponseWriter,
	r *http.Request,
	resource string,
	action string,
) bool {
	// Personal mode is a single-user app with no operator login. The operator
	// RBAC gate is normally orthogonal to multi-tenancy, so it must be
	// short-circuited explicitly here or the dashboard would still demand a
	// login even with auth "off".
	if s.personalMode() {
		return false
	}
	resolve := s.operatorResolver
	if resolve == nil {
		resolve = s.resolveOperatorPrincipal
	}
	principal, err := resolve(r.Context(), r)
	switch {
	case err == nil:
	case errors.Is(err, errNoSession):
		writeJSON(w, http.StatusUnauthorized,
			errEnvelope("OPERATOR_AUTH_REQUIRED", "operator session required"))
		return true
	case errors.Is(err, errOperatorNotAllowed):
		writeJSON(w, http.StatusForbidden,
			errEnvelope("OPERATOR_FORBIDDEN", "session user is not an operator"))
		return true
	default:
		s.log.Warn("admin rbac: operator resolution failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("RBAC_NOT_CONFIGURED", "operator RBAC is not configured"))
		return true
	}

	if s.rbac == nil || !s.rbac.Allowed(principal.Role, resource, action) {
		writeJSON(w, http.StatusForbidden,
			errEnvelope("RBAC_DENIED", "operator role is not allowed to perform this action"))
		return true
	}

	span := trace.SpanFromContext(r.Context())
	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("operator.user_id", principal.UserID),
			attribute.String("operator.role", principal.Role),
			attribute.String("rbac.resource", resource),
			attribute.String("rbac.action", action),
		)
	}
	return false
}

func (s *Server) resolveOperatorPrincipal(ctx context.Context, r *http.Request) (operatorPrincipal, error) {
	if s.db == nil || s.db.Pool == nil {
		return operatorPrincipal{}, errors.New("server: database not configured")
	}
	token := betterAuthSessionToken(r)
	if token == "" {
		// No browser session — fall back to an operator API key so
		// non-interactive callers (af-stack CLI, CI) can reach the
		// operator surface. See resolveOperatorBearer for the rules.
		if p, ok := s.resolveOperatorBearer(ctx, r); ok {
			return p, nil
		}
		return operatorPrincipal{}, errNoSession
	}

	var p operatorPrincipal
	err := s.db.Pool.QueryRow(ctx, `
		select u."id", u."email", coalesce(o.role, '')
		from "session" s
		join "user" u on u."id" = s."userId"
		left join suite_operators o
		  on o.user_id = u."id" or lower(o.email) = lower(u."email")
		where s."token" = $1 and s."expiresAt" > now()
		order by case when o.user_id = u."id" then 0 else 1 end
		limit 1
	`, token).Scan(&p.UserID, &p.Email, &p.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return operatorPrincipal{}, errNoSession
	}
	if err != nil {
		return operatorPrincipal{}, err
	}
	if p.Role == "" {
		return operatorPrincipal{}, errOperatorNotAllowed
	}
	return p, nil
}

// resolveOperatorBearer maps an Authorization Bearer API key onto an
// operator principal. To qualify, the key must be minted on the default
// (zero-uuid) tenant AND carry an explicit operator scope — ordinary
// tenant keys never pass. Scope "operator" grants the admin role
// (read + non-destructive writes); "operator:owner" grants owner.
// Such keys are only mintable through the already-operator-gated
// POST /api/v1/admin/keys or by `af-stack operator key` (direct DB access).
func (s *Server) resolveOperatorBearer(ctx context.Context, r *http.Request) (operatorPrincipal, bool) {
	if s.tenancy == nil {
		return operatorPrincipal{}, false
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const bearer = "Bearer "
	if len(auth) <= len(bearer) || !strings.EqualFold(auth[:len(bearer)], bearer) {
		return operatorPrincipal{}, false
	}
	k, err := s.tenancy.VerifyKey(ctx, strings.TrimSpace(auth[len(bearer):]))
	if err != nil || k.TenantID != tenancy.DefaultTenantID {
		return operatorPrincipal{}, false
	}
	role := ""
	for _, scope := range k.Scopes {
		switch strings.TrimSpace(scope) {
		case "operator:owner":
			role = rbac.RoleOwner
		case "operator":
			if role == "" {
				role = rbac.RoleAdmin
			}
		}
	}
	if role == "" {
		return operatorPrincipal{}, false
	}
	email := ""
	if k.Name != nil {
		email = *k.Name
	}
	return operatorPrincipal{
		UserID: "api-key:" + k.ID,
		Email:  email,
		Role:   role,
	}, true
}

func adminAction(method string) string {
	return rbac.ActionForMethod(method)
}

// callerIsOperator reports whether the request carries a valid operator
// session. Dual-audience endpoints (e.g. jobs, reachable by tenant API
// keys AND the operator dashboard) use it to decide whether cross-tenant
// filters may be honoured.
func (s *Server) callerIsOperator(r *http.Request) bool {
	resolve := s.operatorResolver
	if resolve == nil {
		resolve = s.resolveOperatorPrincipal
	}
	p, err := resolve(r.Context(), r)
	return err == nil && p.Role != ""
}

// operatorGuard wraps h with the operator-auth gate (better-auth session ->
// suite_operators role -> Casbin policy), deriving the RBAC action from the
// HTTP method. It is used to protect operator-only control surfaces that
// bypass the tenant resolver via publicPrefixes (e.g. the DB-studio and
// adapter-registry endpoints) and therefore must enforce their own auth.
//
// Unlike adminAccessDenied, this does NOT require the multi-tenancy module to
// be enabled — operator access is orthogonal to multi-tenancy, the same way
// cost.go and gdpr.go already gate their operator surfaces. s.rbac is always
// wired (server.go falls back to rbac.NewDefault()).
func (s *Server) operatorGuard(resource string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.operatorAccessDenied(w, r, resource, adminAction(r.Method)) {
			return
		}
		h(w, r)
	}
}
