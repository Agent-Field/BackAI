// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
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

func (s *Server) operatorAccessDenied(
	w http.ResponseWriter,
	r *http.Request,
	resource string,
	action string,
) bool {
	principal, err := s.resolveOperatorPrincipal(r.Context(), r)
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

func adminAction(method string) string {
	return rbac.ActionForMethod(method)
}
