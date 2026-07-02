// SPDX-License-Identifier: Apache-2.0

// Package server — secrets.go wires the suite secrets vault to the
// REST endpoints consumed by the dashboard and SDKs.
//
// Every response shape must match the zod schemas in
// apps/dashboard/src/lib/api.ts exactly:
//
//   GET    /api/v1/secrets                       -> SecretListSchema
//   GET    /api/v1/secrets/{key}                 -> SecretMetadataSchema
//   POST   /api/v1/secrets/{key}/reveal           -> SecretValueSchema
//   PUT    /api/v1/secrets/{key}                 -> SecretMetadataSchema (body: PutSecretInput)
//   DELETE /api/v1/secrets/{key}                 -> {"deleted": true}
//   POST   /api/v1/secrets/{key}/rotate           -> SecretMetadataSchema (body: {"value": string})
//
// Plaintext only ever leaves the runtime via /reveal. List and Get
// responses MUST NOT include the value field.
//
// Phase 5: tenant resolution is hardcoded to the seeded "default"
// tenant. Phase 6 will replace defaultTenant() with a resolver that
// reads from the auth context.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// defaultTenantUUID is the well-known UUID seeded by migration 00001
// for the "default" tenant. Used until Phase 6 wires real tenant
// resolution from auth context.
const defaultTenantUUID = "00000000-0000-0000-0000-000000000000"

// defaultTenant returns the tenant ID to use for an unauthenticated /
// single-tenant request. Centralised so Phase 6 can swap in a real
// resolver without hunting through every handler.
func (s *Server) defaultTenant(_ *http.Request) string {
	return defaultTenantUUID
}

// registerSecretsRoutes wires the secrets endpoints. Called from
// registerRoutes() in server.go.
func (s *Server) registerSecretsRoutes() {
	// S1b: the secrets vault bypasses the tenant resolver (publicPrefixes) and
	// operates on a single hardcoded tenant — an unauthenticated caller could
	// list and /reveal plaintext secrets. Gate the whole surface to operators
	// (the dashboard manages secrets via its better-auth session). Per-tenant
	// secrets via API key is a separate follow-up.
	guard := func(res string, h http.HandlerFunc) http.HandlerFunc { return s.operatorGuard(res, h) }
	s.mux.HandleFunc("GET /api/v1/secrets", guard(rbac.ResourceAdminSecrets, s.handleListSecrets))
	s.mux.HandleFunc("GET /api/v1/secrets/{key}", guard(rbac.ResourceAdminSecrets, s.handleGetSecretMetadata))
	s.mux.HandleFunc("PUT /api/v1/secrets/{key}", guard(rbac.ResourceAdminSecrets, s.handlePutSecret))
	s.mux.HandleFunc("DELETE /api/v1/secrets/{key}", guard(rbac.ResourceAdminSecrets, s.handleDeleteSecret))
	s.mux.HandleFunc("POST /api/v1/secrets/{key}/reveal", guard(rbac.ResourceAdminSecrets, s.handleRevealSecret))
	s.mux.HandleFunc("POST /api/v1/secrets/{key}/rotate", guard(rbac.ResourceAdminSecrets, s.handleRotateSecret))
}

// parseRFC3339 tolerates both RFC3339 and RFC3339Nano so clients can
// send either flavour.
func parseRFC3339(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// vaultUnavailable returns true (and writes a 503) if the secrets vault
// is not wired up. The handlers degrade rather than panic so a runtime
// without DB / KEK still serves /health and other endpoints.
func (s *Server) vaultUnavailable(w http.ResponseWriter) bool {
	if s.secrets == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("SECRETS_NOT_CONFIGURED",
				"secrets vault is not configured on this runtime"))
		return true
	}
	return false
}

// writeSecretError maps secrets package errors to HTTP responses.
func writeSecretError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, secrets.ErrSecretNotFound):
		writeJSON(w, http.StatusNotFound,
			errEnvelope("NOT_FOUND", "secret not found"))
	case errors.Is(err, secrets.ErrInvalidKey):
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "invalid secret key"))
	default:
		writeJSON(w, http.StatusInternalServerError,
			errEnvelope("INTERNAL", "secrets operation failed"))
	}
}

// ─── GET /api/v1/secrets ──────────────────────────────────────────────────

type secretListResponse struct {
	Secrets []secrets.SecretMetadata `json:"secrets"`
}

func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.secrets.list")
	defer span.End()
	if s.vaultUnavailable(w) {
		return
	}
	items, err := s.secrets.List(ctx, s.defaultTenant(r))
	if err != nil {
		s.log.Error("secrets: list failed", "error", err)
		span.RecordError(err)
		writeSecretError(w, err)
		return
	}
	if items == nil {
		items = []secrets.SecretMetadata{}
	}
	writeJSON(w, http.StatusOK, secretListResponse{Secrets: items})
}

// ─── GET /api/v1/secrets/{key} ────────────────────────────────────────────

func (s *Server) handleGetSecretMetadata(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.secrets.get")
	defer span.End()
	if s.vaultUnavailable(w) {
		return
	}
	key := r.PathValue("key")
	meta, err := s.secrets.GetMetadata(ctx, s.defaultTenant(r), key)
	if err != nil {
		span.RecordError(err)
		writeSecretError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// ─── POST /api/v1/secrets/{key}/reveal ────────────────────────────────────

type secretValueResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (s *Server) handleRevealSecret(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.secrets.reveal")
	defer span.End()
	if s.vaultUnavailable(w) {
		return
	}
	key := r.PathValue("key")
	tenantID := s.defaultTenant(r)
	plaintext, err := s.secrets.Get(ctx, tenantID, key)
	if err != nil {
		// Failed reveal attempts are an interesting audit signal too,
		// but only when the failure isn't "wrong key shape" or "no
		// such secret" (those happen on every dashboard typo). We log
		// only the success path for now; failures stay in the slog.
		span.RecordError(err)
		writeSecretError(w, err)
		return
	}
	// The plaintext leaves the runtime here. We do NOT log the response
	// body; the only sink is the HTTP writer.
	// Audit log: every successful reveal is a sensitive event. Best-
	// effort write so the response isn't blocked on a slow audit
	// insert; the audit ctx is decoupled from the response ctx.
	s.recordSecretReveal(r, tenantID, key)
	writeJSON(w, http.StatusOK, secretValueResponse{
		Key:   key,
		Value: string(plaintext),
	})
}

// recordSecretReveal inserts an audit row noting that a secret was
// revealed via the dashboard endpoint. Runs in a goroutine with a
// fresh, short-lived context so a slow DB write doesn't block the
// caller; on a missing pool (tests / degraded mode) it's a no-op.
//
// Action: "secret.reveal" — the dashboard's audit panel filters on
// this string. Resource: ("secret", key). Metadata captures the path
// + method for cross-correlation with suite_gateway_requests rows.
func (s *Server) recordSecretReveal(r *http.Request, tenantID, key string) {
	if s.db == nil || s.db.Pool == nil {
		return
	}
	userID := tenantctx.UserID(r.Context())
	apiKeyID := tenantctx.APIKeyID(r.Context())
	ip := clientIP(r)
	userAgent := r.Header.Get("User-Agent")

	var tenantPtr, userPtr, keyPtr, ipPtr, uaPtr *string
	if tenantID != "" {
		tenantPtr = &tenantID
	}
	if userID != "" {
		userPtr = &userID
	}
	if apiKeyID != "" {
		keyPtr = &apiKeyID
	}
	if ip != "" {
		ipPtr = &ip
	}
	if userAgent != "" {
		userAgent = truncateAudit(userAgent, 512)
		uaPtr = &userAgent
	}
	resourceID := key

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Bind the write connection to the row's tenant so the audit-log RLS
		// WITH CHECK passes (the pool's PrepareConn hook reads ctx). Secrets
		// are single-tenant today (default tenant), but bind explicitly so
		// this keeps working once per-tenant secrets land.
		ctx = tenantctx.WithTenant(ctx, tenantID, apiKeyID)
		metadata := map[string]any{
			"path":   r.URL.Path,
			"method": r.Method,
		}
		metaBytes, _ := json.Marshal(metadata)
		_, err := s.db.Pool.Exec(ctx, `
			insert into suite_audit_log
				(tenant_id, user_id, api_key_id, action,
				 resource_type, resource_id, metadata, ip, user_agent)
			values
				($1, $2, $3, 'secret.reveal', 'secret', $4, $5, $6, $7)
		`, tenantPtr, userPtr, keyPtr, resourceID, metaBytes, ipPtr, uaPtr)
		if err != nil {
			s.log.Warn("secrets: audit insert failed",
				"key", key, "tenant_id", tenantID, "error", err)
		}
	}()
}

// clientIP returns the closest thing to a remote client IP we can
// derive. Prefers X-Forwarded-For (first value), then X-Real-IP, then
// the raw RemoteAddr. Returns "" rather than half-baked data when no
// signal is present.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := indexComma(v); i >= 0 {
			return trimSpace(v[:i])
		}
		return trimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return trimSpace(v)
	}
	host := r.RemoteAddr
	// RemoteAddr may be "ip:port"; strip the port.
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return host[:i]
		}
	}
	return host
}

func indexComma(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// truncateAudit clips long strings so a hostile User-Agent can't bloat
// the audit row. RFC 7230 doesn't bound the header but 512 is plenty
// for legitimate clients.
func truncateAudit(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// ─── PUT /api/v1/secrets/{key} ────────────────────────────────────────────

// putSecretInput mirrors PutSecretInputSchema in api.ts.
type putSecretInput struct {
	Value       string  `json:"value"`
	Description *string `json:"description,omitempty"`
	RotateAfter *string `json:"rotate_after,omitempty"`
}

func (s *Server) handlePutSecret(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.secrets.put")
	defer span.End()
	if s.vaultUnavailable(w) {
		return
	}
	key := r.PathValue("key")

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("BAD_REQUEST", "could not read body"))
		return
	}
	var in putSecretInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "invalid JSON body"))
		return
	}
	if in.Value == "" {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "value is required"))
		return
	}

	putIn := secrets.PutInput{Value: in.Value}
	if in.Description != nil {
		putIn.Description = *in.Description
	}
	if in.RotateAfter != nil && *in.RotateAfter != "" {
		t, perr := parseRFC3339(*in.RotateAfter)
		if perr == nil {
			putIn.RotateAfter = &t
		}
	}

	meta, err := s.secrets.Put(ctx, s.defaultTenant(r), key, putIn)
	if err != nil {
		span.RecordError(err)
		writeSecretError(w, err)
		return
	}
	s.audit.Write(ctx, r, audit.Event{
		Action:       "secret.put",
		ResourceType: "secret",
		ResourceID:   key,
	})
	writeJSON(w, http.StatusOK, meta)
}

// ─── DELETE /api/v1/secrets/{key} ─────────────────────────────────────────

func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.secrets.delete")
	defer span.End()
	if s.vaultUnavailable(w) {
		return
	}
	key := r.PathValue("key")
	if err := s.secrets.Delete(ctx, s.defaultTenant(r), key); err != nil {
		span.RecordError(err)
		writeSecretError(w, err)
		return
	}
	s.audit.Write(ctx, r, audit.Event{
		Action:       "secret.delete",
		ResourceType: "secret",
		ResourceID:   key,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ─── POST /api/v1/secrets/{key}/rotate ────────────────────────────────────

type rotateInput struct {
	Value string `json:"value"`
}

func (s *Server) handleRotateSecret(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.secrets.rotate")
	defer span.End()
	if s.vaultUnavailable(w) {
		return
	}
	key := r.PathValue("key")

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("BAD_REQUEST", "could not read body"))
		return
	}
	var in rotateInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "invalid JSON body"))
		return
	}
	if in.Value == "" {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "value is required"))
		return
	}

	meta, err := s.secrets.Rotate(ctx, s.defaultTenant(r), key, in.Value)
	if err != nil {
		span.RecordError(err)
		writeSecretError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}