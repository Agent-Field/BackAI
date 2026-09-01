// SPDX-License-Identifier: Apache-2.0

// Package server — tenant_secrets.go wires the per-tenant secrets vault
// to the REST endpoints consumed by the SDKs over a tenant bearer key.
//
// This is a SEPARATE surface from the operator vault in secrets.go:
//
//	Operator surface (secrets.go)      Tenant surface (this file)
//	  GET/PUT/DELETE /api/v1/secrets     GET/PUT/DELETE /api/v1/vault/secrets
//	  operatorGuard (better-auth)        tenant resolver (API key / session)
//	  hardcoded default tenant           the caller's resolved tenant
//
// The operator surface stays gated + audited exactly as before; nothing
// here touches it. The tenant surface rides the tenant resolver:
// /api/v1/vault/* is deliberately NOT in publicPrefixes, so
// tenantResolver binds the caller's tenant (from the Authorization bearer
// key or session cookie) into the request context before any handler
// runs. Every operation is then scoped to that tenant — the vault's
// WHERE tenant_id clause AND the FORCE ROW LEVEL SECURITY policy on
// suite_secrets (migration 00004_rls.sql) both key off it.
//
// Plaintext only ever leaves the runtime via the explicit /reveal
// contract (audited). List and get-metadata responses carry a
// "secret:<key>" reference the caller can drop into config (e.g. an MCP
// server env value) instead of the value itself.
package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// secretRefPrefix is the reference scheme the runtime resolves elsewhere
// (see internal/mcp/secrets.go). A get-metadata / list response hands the
// caller "secret:<key>" so app config can reference the value without the
// plaintext ever transiting.
const secretRefPrefix = "secret:"

// registerTenantSecretsRoutes wires the tenant secrets endpoints. Called
// from registerRoutes() in server.go. These paths bypass operatorGuard —
// authorization is the tenant resolver binding a real tenant.
func (s *Server) registerTenantSecretsRoutes() {
	s.mux.HandleFunc("GET /api/v1/vault/secrets", s.handleTenantListSecrets)
	s.mux.HandleFunc("GET /api/v1/vault/secrets/{key}", s.handleTenantGetSecretMetadata)
	s.mux.HandleFunc("PUT /api/v1/vault/secrets/{key}", s.handleTenantPutSecret)
	s.mux.HandleFunc("DELETE /api/v1/vault/secrets/{key}", s.handleTenantDeleteSecret)
	s.mux.HandleFunc("POST /api/v1/vault/secrets/{key}/reveal", s.handleTenantRevealSecret)
	s.mux.HandleFunc("POST /api/v1/vault/secrets/{key}/rotate", s.handleTenantRotateSecret)
	s.registerTenantSecretsOpenAPI()
}

// tenantSecretStore returns the secrets backend the tenant handlers use.
// The test-only s.secretStore override wins when set; otherwise it falls
// back to the concrete vault, returning an untyped nil (not a typed-nil
// interface) when no vault is wired so the 503 path stays intact.
func (s *Server) tenantSecretStore() secrets.Store {
	if s.secretStore != nil {
		return s.secretStore
	}
	if s.secrets == nil {
		return nil
	}
	return s.secrets
}

// tenantSecretsReady writes a 503 and returns ok=false when no vault is
// configured, mirroring the operator surface's degrade-don't-panic
// behaviour.
func (s *Server) tenantSecretsReady(w http.ResponseWriter) (secrets.Store, bool) {
	store := s.tenantSecretStore()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("SECRETS_NOT_CONFIGURED",
				"secrets vault is not configured on this runtime"))
		return nil, false
	}
	return store, true
}

// resolvedTenantOrDeny returns the tenant bound by the resolver. It is a
// defence-in-depth guard: /api/v1/vault/* is not public, so the resolver
// either binds a tenant or 401s before we get here — but if the context
// ever arrives tenant-less we refuse rather than silently touch another
// tenant's rows or fall back to the default tenant.
func (s *Server) resolvedTenantOrDeny(w http.ResponseWriter, r *http.Request) (string, bool) {
	tid := tenantctx.TenantID(r.Context())
	if tid == "" {
		writeJSON(w, http.StatusUnauthorized,
			errEnvelope("UNAUTHENTICATED", "missing or invalid credentials"))
		return "", false
	}
	return tid, true
}

// tenantSecretMetadata is the tenant-surface view: the shared metadata
// shape plus the "secret:<key>" reference. Embedding flattens the
// SecretMetadata fields into the same JSON object, so the wire shape is a
// superset of the operator surface's (never a plaintext value).
type tenantSecretMetadata struct {
	secrets.SecretMetadata
	Reference string `json:"reference"`
}

func withReference(m secrets.SecretMetadata) tenantSecretMetadata {
	return tenantSecretMetadata{SecretMetadata: m, Reference: secretRefPrefix + m.Key}
}

type tenantSecretListResponse struct {
	Secrets []tenantSecretMetadata `json:"secrets"`
}

// ─── GET /api/v1/vault/secrets ─────────────────────────────────────────────

func (s *Server) handleTenantListSecrets(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "vault.secrets.list")
	defer span.End()
	store, ok := s.tenantSecretsReady(w)
	if !ok {
		return
	}
	tenantID, ok := s.resolvedTenantOrDeny(w, r)
	if !ok {
		return
	}
	items, err := store.List(ctx, tenantID)
	if err != nil {
		s.log.Error("vault secrets: list failed", "error", err)
		span.RecordError(err)
		writeSecretError(w, err)
		return
	}
	out := make([]tenantSecretMetadata, 0, len(items))
	for _, m := range items {
		out = append(out, withReference(m))
	}
	writeJSON(w, http.StatusOK, tenantSecretListResponse{Secrets: out})
}

// ─── GET /api/v1/vault/secrets/{key} ───────────────────────────────────────

func (s *Server) handleTenantGetSecretMetadata(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "vault.secrets.get")
	defer span.End()
	store, ok := s.tenantSecretsReady(w)
	if !ok {
		return
	}
	tenantID, ok := s.resolvedTenantOrDeny(w, r)
	if !ok {
		return
	}
	meta, err := store.GetMetadata(ctx, tenantID, r.PathValue("key"))
	if err != nil {
		span.RecordError(err)
		writeSecretError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, withReference(meta))
}

// ─── PUT /api/v1/vault/secrets/{key} ───────────────────────────────────────

func (s *Server) handleTenantPutSecret(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "vault.secrets.put")
	defer span.End()
	store, ok := s.tenantSecretsReady(w)
	if !ok {
		return
	}
	tenantID, ok := s.resolvedTenantOrDeny(w, r)
	if !ok {
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
		if t, perr := parseRFC3339(*in.RotateAfter); perr == nil {
			putIn.RotateAfter = &t
		}
	}

	meta, err := store.Put(ctx, tenantID, key, putIn)
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
	writeJSON(w, http.StatusOK, withReference(meta))
}

// ─── DELETE /api/v1/vault/secrets/{key} ────────────────────────────────────

func (s *Server) handleTenantDeleteSecret(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "vault.secrets.delete")
	defer span.End()
	store, ok := s.tenantSecretsReady(w)
	if !ok {
		return
	}
	tenantID, ok := s.resolvedTenantOrDeny(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")
	if err := store.Delete(ctx, tenantID, key); err != nil {
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

// ─── POST /api/v1/vault/secrets/{key}/reveal ───────────────────────────────

func (s *Server) handleTenantRevealSecret(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "vault.secrets.reveal")
	defer span.End()
	store, ok := s.tenantSecretsReady(w)
	if !ok {
		return
	}
	tenantID, ok := s.resolvedTenantOrDeny(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")
	plaintext, err := store.Get(ctx, tenantID, key)
	if err != nil {
		span.RecordError(err)
		writeSecretError(w, err)
		return
	}
	// Audit every successful reveal — the plaintext leaves the runtime
	// here. recordSecretReveal is shared with the operator surface and is
	// already tenant-parameterised.
	s.recordSecretReveal(r, tenantID, key)
	writeJSON(w, http.StatusOK, secretValueResponse{Key: key, Value: string(plaintext)})
}

// ─── POST /api/v1/vault/secrets/{key}/rotate ───────────────────────────────

func (s *Server) handleTenantRotateSecret(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "vault.secrets.rotate")
	defer span.End()
	store, ok := s.tenantSecretsReady(w)
	if !ok {
		return
	}
	tenantID, ok := s.resolvedTenantOrDeny(w, r)
	if !ok {
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

	meta, err := store.Rotate(ctx, tenantID, key, in.Value)
	if err != nil {
		span.RecordError(err)
		writeSecretError(w, err)
		return
	}
	s.audit.Write(ctx, r, audit.Event{
		Action:       "secret.rotate",
		ResourceType: "secret",
		ResourceID:   key,
	})
	writeJSON(w, http.StatusOK, withReference(meta))
}

// registerTenantSecretsOpenAPI describes /api/v1/vault/secrets/* routes.
func (s *Server) registerTenantSecretsOpenAPI() {
	b := s.openapi
	if b == nil {
		return
	}
	b.Register("GET", "/api/v1/vault/secrets", openapi.RouteMeta{
		Summary: "List the caller tenant's secrets (metadata + reference only)",
		Tags:    []string{"secrets"},
	})
	b.Register("GET", "/api/v1/vault/secrets/{key}", openapi.RouteMeta{
		Summary: "Get secret metadata + reference (no value)", Tags: []string{"secrets"},
	})
	b.Register("PUT", "/api/v1/vault/secrets/{key}", openapi.RouteMeta{
		Summary: "Create or replace one of the caller tenant's secrets", Tags: []string{"secrets"},
	})
	b.Register("DELETE", "/api/v1/vault/secrets/{key}", openapi.RouteMeta{
		Summary: "Delete one of the caller tenant's secrets", Tags: []string{"secrets"},
	})
	b.Register("POST", "/api/v1/vault/secrets/{key}/reveal", openapi.RouteMeta{
		Summary: "Reveal the plaintext value (audited)", Tags: []string{"secrets"},
	})
	b.Register("POST", "/api/v1/vault/secrets/{key}/rotate", openapi.RouteMeta{
		Summary: "Rotate the stored value", Tags: []string{"secrets"},
	})
}
