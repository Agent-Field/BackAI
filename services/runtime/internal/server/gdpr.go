// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
)

type gdprExportResponse struct {
	ExportedAt        string         `json:"exported_at"`
	UserID            string         `json:"user_id"`
	AgentFieldNotice  string         `json:"agentfield_notice"`
	Data              map[string]any `json:"data"`
	RedactionContract string         `json:"redaction_contract"`
}

type gdprEraseResponse struct {
	UserID           string           `json:"user_id"`
	ErasedAt         string           `json:"erased_at"`
	Counts           map[string]int64 `json:"counts"`
	AgentFieldNotice string           `json:"agentfield_notice"`
}

func (s *Server) registerGDPRRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/users/{id}/export", s.handleAdminExportUserData)
	s.mux.HandleFunc("POST /api/v1/admin/users/{id}/erase", s.handleAdminEraseUserData)
	if s.openapi != nil {
		tags := []string{"admin"}
		s.openapi.Register("GET", "/api/v1/admin/users/{id}/export", openapi.RouteMeta{
			Summary: "Export AF Stack-held data for a user", Tags: tags,
		})
		s.openapi.Register("POST", "/api/v1/admin/users/{id}/erase", openapi.RouteMeta{
			Summary: "Erase/anonymize AF Stack-held data for a user", Tags: tags,
		})
	}
}

func (s *Server) gdprUnavailable(w http.ResponseWriter) bool {
	if s.db == nil || s.db.Pool == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("GDPR_NOT_CONFIGURED", "database is not configured on this runtime"))
		return true
	}
	return false
}

func (s *Server) handleAdminExportUserData(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.gdpr.export_user")
	defer span.End()
	if s.gdprUnavailable(w) {
		return
	}
	if s.operatorAccessDenied(w, r, rbac.ResourceAdminPrivacy, rbac.ActionRead) {
		return
	}
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "user id required"))
		return
	}
	email, err := s.lookupSuiteUserEmail(ctx, userID)
	if err != nil {
		writeGDPRError(w, err)
		return
	}
	data, err := s.exportSuiteUser(ctx, userID, email)
	if err != nil {
		span.RecordError(err)
		writeGDPRError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gdprExportResponse{
		ExportedAt: time.Now().UTC().Format(time.RFC3339Nano),
		UserID:     userID,
		AgentFieldNotice: "AF Stack export includes AF Stack app-auth/backend records only. " +
			"AgentField-owned runs, spans, traces, sessions, and memory remain in AgentField.",
		RedactionContract: "Secret values and OAuth token plaintext are never exported; only metadata and vault references are included.",
		Data:              data,
	})
}

func (s *Server) handleAdminEraseUserData(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.gdpr.erase_user")
	defer span.End()
	if s.gdprUnavailable(w) {
		return
	}
	if s.operatorAccessDenied(w, r, rbac.ResourceAdminPrivacy, rbac.ActionDelete) {
		return
	}
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "user id required"))
		return
	}
	email, err := s.lookupSuiteUserEmail(ctx, userID)
	if err != nil {
		writeGDPRError(w, err)
		return
	}
	counts, err := s.eraseSuiteUser(ctx, userID, email)
	if err != nil {
		span.RecordError(err)
		writeGDPRError(w, err)
		return
	}
	s.audit.Write(ctx, r, audit.Event{
		Action:       "gdpr.user.erase",
		ResourceType: "user",
		ResourceID:   userID,
		Metadata: map[string]any{
			"counts": counts,
		},
	})
	writeJSON(w, http.StatusOK, gdprEraseResponse{
		UserID:   userID,
		ErasedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Counts:   counts,
		AgentFieldNotice: "AF Stack erased/anonymized AF Stack app-auth/backend records only. " +
			"AgentField-owned runs, spans, traces, sessions, and memory remain in AgentField.",
	})
}

func (s *Server) lookupSuiteUserEmail(ctx context.Context, userID string) (string, error) {
	var email string
	err := s.db.Pool.QueryRow(ctx, `
		select email from suite_users where id = $1::uuid limit 1
	`, userID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", pgx.ErrNoRows
	}
	return email, err
}

func (s *Server) exportSuiteUser(ctx context.Context, userID, email string) (map[string]any, error) {
	queries := map[string]string{
		"suite_user": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select id::text, email, name, avatar_url, created_at, deleted_at
			      from suite_users where id = $1::uuid) row`,
		"better_auth_user": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select id, name, email, "emailVerified", image, "createdAt", "updatedAt"
			      from "user" where lower(email) = lower($2)) row`,
		"better_auth_accounts": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select a.id, a."accountId", a."providerId", a."userId", a.scope, a."createdAt", a."updatedAt"
			      from "account" a join "user" u on u.id = a."userId"
			      where lower(u.email) = lower($2)) row`,
		"memberships": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select tenant_id::text, user_id::text, role, invited_at, accepted_at
			      from suite_memberships where user_id = $1::uuid) row`,
		"api_keys_created": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select id::text, tenant_id::text, prefix, name, scopes, created_at, last_used_at, expires_at, revoked_at
			      from suite_api_keys where created_by = $1::uuid) row`,
		"gateway_requests": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select id::text, tenant_id::text, api_key_id::text, user_id::text, endpoint, method, status_code,
			             duration_ms, request_bytes, response_bytes, af_execution_id, request_id, ip::text, user_agent, created_at
			      from suite_gateway_requests where user_id = $1::uuid order by created_at desc limit 1000) row`,
		"audit_log": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select id::text, tenant_id::text, user_id::text, api_key_id::text, action, resource_type, resource_id,
			             metadata, ip::text, user_agent, occurred_at
			      from suite_audit_log where user_id = $1::uuid order by occurred_at desc limit 1000) row`,
		"user_activity": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select id::text, tenant_id::text, user_id::text, api_key_id::text, actor_type, action, resource_type,
			             resource_id, metadata, ip::text, user_agent, occurred_at
			      from suite_user_activity where user_id = $1::uuid order by occurred_at desc limit 1000) row`,
		"oauth_connections": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select id::text, tenant_id::text, user_id::text, provider, access_token_ref, refresh_token_ref,
			             scopes, expires_at, created_at, updated_at, revoked_at
			      from suite_oauth_tokens where user_id = $1::uuid) row`,
		"feature_flags_updated": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select tenant_id::text, key, enabled, value, updated_by::text, updated_at
			      from suite_feature_flags where updated_by = $1::uuid) row`,
		"tool_adapters_updated": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select tenant_id::text, adapter_id, enabled, config, updated_by::text, updated_at
			      from suite_tool_adapters where updated_by = $1::uuid) row`,
		"shipwright_tasks": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select id::text, tenant_id::text, user_id::text, title, description, repo_url, status, run_id, created_at, updated_at
			      from suite_shipwright_tasks where user_id = $1::uuid order by created_at desc limit 1000) row`,
		"approvals_requested": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select id::text, tenant_id::text, requested_by::text, kind, payload, status, decided_by::text, decided_at, created_at
			      from suite_approvals where requested_by = $1::uuid order by created_at desc limit 1000) row`,
		"approvals_decided": `select coalesce(jsonb_agg(to_jsonb(row)), '[]'::jsonb)
			from (select id::text, tenant_id::text, requested_by::text, kind, payload, status, decided_by::text, decided_at, created_at
			      from suite_approvals where decided_by = $1::uuid order by created_at desc limit 1000) row`,
	}

	out := make(map[string]any, len(queries))
	for name, query := range queries {
		rows, err := s.queryGDPRRows(ctx, query, userID, email)
		if err != nil {
			return nil, fmt.Errorf("gdpr: export %s: %w", name, err)
		}
		out[name] = rows
	}
	return out, nil
}

func (s *Server) queryGDPRRows(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	var raw []byte
	err := s.db.Pool.QueryRow(ctx, query, args...).Scan(&raw)
	if ignoreOptionalGDPRError(err) {
		return []map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

func (s *Server) eraseSuiteUser(ctx context.Context, userID, email string) (map[string]int64, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// The erase is a by-user-id sweep across every tenant the user belongs
	// to (memberships, oauth tokens, and updated_by/actor references span
	// tenants; the oauth-secrets CTE resolves tenant_id per row). The
	// /api/v1/admin surface bypasses the tenant resolver, so no single
	// app.tenant_id can scope these deletes — bind-to-tenant would make the
	// erase a silent no-op. Bypass force-RLS transactionally (same committed
	// pattern as server/sql_history.go) so the operator-gated erase can reach
	// the user's rows in all tenants.
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		return nil, err
	}

	counts := map[string]int64{}
	exec := func(name, sql string, args ...any) error {
		tag, err := tx.Exec(ctx, sql, args...)
		if ignoreOptionalGDPRError(err) {
			counts[name] = 0
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		counts[name] = tag.RowsAffected()
		return nil
	}

	var authUserID string
	_ = tx.QueryRow(ctx, `select id from "user" where lower(email) = lower($1) limit 1`, email).Scan(&authUserID)

	if err := exec("oauth_token_secrets_deleted", `
		with refs as (
		  select tenant_id, access_token_ref as key from suite_oauth_tokens where user_id = $1::uuid
		  union all
		  select tenant_id, refresh_token_ref as key from suite_oauth_tokens
		    where user_id = $1::uuid and refresh_token_ref is not null
		)
		delete from suite_secrets s using refs
		where s.tenant_id = refs.tenant_id and s.key = refs.key
	`, userID); err != nil {
		return nil, err
	}
	statements := []struct {
		name string
		sql  string
		args []any
	}{
		{"oauth_connections_deleted", `delete from suite_oauth_tokens where user_id = $1::uuid`, []any{userID}},
		{"memberships_deleted", `delete from suite_memberships where user_id = $1::uuid`, []any{userID}},
		{"sessions_deleted", `delete from "session" where "userId" in (
			select id from "user" where lower(email) = lower($2)
		)`, []any{userID, email}},
		{"accounts_deleted", `delete from "account" where "userId" in (
			select id from "user" where lower(email) = lower($2)
		)`, []any{userID, email}},
		{"auth_users_deleted", `delete from "user" where lower(email) = lower($2)`, []any{userID, email}},
		{"operators_deleted", `delete from suite_operators
			where lower(email) = lower($2) or ($3 <> '' and user_id = $3)`, []any{userID, email, authUserID}},
		{"api_keys_created_by_cleared", `update suite_api_keys set created_by = null where created_by = $1::uuid`, []any{userID}},
		{"gateway_requests_user_cleared", `update suite_gateway_requests set user_id = null where user_id = $1::uuid`, []any{userID}},
		{"audit_user_cleared", `update suite_audit_log set user_id = null where user_id = $1::uuid`, []any{userID}},
		{"user_activity_user_cleared", `update suite_user_activity set user_id = null where user_id = $1::uuid`, []any{userID}},
		{"feature_flags_updated_by_cleared", `update suite_feature_flags set updated_by = null
			where updated_by = $1::uuid`, []any{userID}},
		{"tool_adapters_updated_by_cleared", `update suite_tool_adapters set updated_by = null
			where updated_by = $1::uuid`, []any{userID}},
		{"shipwright_tasks_user_cleared", `update suite_shipwright_tasks set user_id = null where user_id = $1::uuid`, []any{userID}},
		{"approvals_requested_by_cleared", `update suite_approvals set requested_by = null
			where requested_by = $1::uuid`, []any{userID}},
		{"approvals_decided_by_cleared", `update suite_approvals set decided_by = null
			where decided_by = $1::uuid`, []any{userID}},
		{"suite_user_anonymized", `update suite_users
			set email = 'erased-' || id::text || '@erased.af-stack.local',
			    name = null,
			    avatar_url = null,
			    deleted_at = coalesce(deleted_at, now())
			where id = $1::uuid`, []any{userID}},
	}
	for _, stmt := range statements {
		if err := exec(stmt.name, stmt.sql, stmt.args...); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return counts, nil
}

func writeGDPRError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusNotFound, errEnvelope("NOT_FOUND", "user not found"))
	default:
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", "GDPR operation failed"))
	}
}

func ignoreOptionalGDPRError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "42P01" || pgErr.Code == "42703")
}
