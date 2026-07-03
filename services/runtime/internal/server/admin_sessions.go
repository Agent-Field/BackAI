// SPDX-License-Identifier: Apache-2.0

// admin_sessions.go — operator view of live better-auth sessions.
//
// The dashboard and customer-app share the same better-auth tables in the
// same Postgres ("session"/"user", created by 00002_better_auth.sql — see
// apps/customer-app/src/lib/auth.ts). This surface lists active sessions
// across both apps and lets an operator revoke one. Operator vs customer
// is derived by joining suite_operators, not by table — the cookie prefix
// only differs in the browser.
//
// The better-auth tables carry no RLS (they are not suite_* tables), so
// plain pool queries are correct here; auth is enforced by operatorGuard.
package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
)

type sessionWire struct {
	ID         string  `json:"id"`
	UserID     string  `json:"user_id"`
	Email      string  `json:"email"`
	Name       *string `json:"name"`
	IPAddress  *string `json:"ip_address"`
	UserAgent  *string `json:"user_agent"`
	IsOperator bool    `json:"is_operator"`
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  string  `json:"expires_at"`
}

type sessionListResponse struct {
	Sessions []sessionWire `json:"sessions"`
	Total    int           `json:"total"`
	HasMore  bool          `json:"has_more"`
}

func (s *Server) registerAdminSessionsRoutes() {
	// S1b posture: /api/v1/admin is on publicPrefixes, so the handlers
	// enforce operator auth themselves.
	s.mux.HandleFunc("GET /api/v1/admin/sessions",
		s.operatorGuard(rbac.ResourceAdminSessions, s.handleAdminListSessions))
	s.mux.HandleFunc("DELETE /api/v1/admin/sessions/{id}",
		s.operatorGuard(rbac.ResourceAdminSessions, s.handleAdminRevokeSession))
}

func (s *Server) registerAdminSessionsOpenAPI() {
	s.openapi.Register("GET", "/api/v1/admin/sessions", openapi.RouteMeta{
		Summary: "List active better-auth sessions (operator + customer)",
		Tags:    []string{"admin"},
	})
	s.openapi.Register("DELETE", "/api/v1/admin/sessions/{id}", openapi.RouteMeta{
		Summary: "Revoke a session by id",
		Tags:    []string{"admin"},
	})
}

func (s *Server) handleAdminListSessions(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.admin.sessions.list")
	defer span.End()
	if s.db == nil || s.db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "DB_NOT_CONFIGURED",
			"sessions require a database", nil)
		return
	}

	q := r.URL.Query()
	limit, offset := parsePaging(q.Get("limit"), q.Get("offset"))
	conds := []string{`s."expiresAt" > now()`}
	args := []any{}
	bind := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if strings.EqualFold(strings.TrimSpace(q.Get("include_expired")), "true") {
		conds = []string{"true"}
	}
	if email := strings.TrimSpace(q.Get("email")); email != "" {
		conds = append(conds, `u."email" ilike `+bind("%"+email+"%"))
	}
	where := " where " + strings.Join(conds, " and ")

	base := `
		from "session" s
		join "user" u on u."id" = s."userId"` + where

	var total int
	if err := s.db.Pool.QueryRow(ctx, "select count(*) "+base, args...).Scan(&total); err != nil {
		span.RecordError(err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}

	pageArgs := append([]any{}, args...)
	pageArgs = append(pageArgs, limit, offset)
	n := len(pageArgs)
	rows, err := s.db.Pool.Query(ctx, `
		select s."id", s."userId", u."email", u."name",
		       s."ipAddress", s."userAgent",
		       exists(
		         select 1 from suite_operators o
		         where o.user_id = u."id" or lower(o.email) = lower(u."email")
		       ) as is_operator,
		       s."createdAt", s."expiresAt" `+base+`
		order by s."createdAt" desc
		limit $`+strconv.Itoa(n-1)+` offset $`+strconv.Itoa(n), pageArgs...)
	if err != nil {
		span.RecordError(err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	defer rows.Close()

	resp := sessionListResponse{Sessions: []sessionWire{}, Total: total}
	for rows.Next() {
		var (
			sw                 sessionWire
			createdAt, expires time.Time
		)
		if err := rows.Scan(&sw.ID, &sw.UserID, &sw.Email, &sw.Name,
			&sw.IPAddress, &sw.UserAgent, &sw.IsOperator, &createdAt, &expires); err != nil {
			span.RecordError(err)
			writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
			return
		}
		sw.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		sw.ExpiresAt = expires.UTC().Format(time.RFC3339Nano)
		resp.Sessions = append(resp.Sessions, sw)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	resp.HasMore = offset+len(resp.Sessions) < total
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminRevokeSession(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.admin.sessions.revoke")
	defer span.End()
	if s.db == nil || s.db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "DB_NOT_CONFIGURED",
			"sessions require a database", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "session id required", nil)
		return
	}
	tag, err := s.db.Pool.Exec(ctx, `delete from "session" where "id" = $1`, id)
	if err != nil {
		span.RecordError(err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "session not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}
