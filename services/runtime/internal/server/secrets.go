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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
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
	s.mux.HandleFunc("GET /api/v1/secrets", s.handleListSecrets)
	s.mux.HandleFunc("GET /api/v1/secrets/{key}", s.handleGetSecretMetadata)
	s.mux.HandleFunc("PUT /api/v1/secrets/{key}", s.handlePutSecret)
	s.mux.HandleFunc("DELETE /api/v1/secrets/{key}", s.handleDeleteSecret)
	s.mux.HandleFunc("POST /api/v1/secrets/{key}/reveal", s.handleRevealSecret)
	s.mux.HandleFunc("POST /api/v1/secrets/{key}/rotate", s.handleRotateSecret)
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
	plaintext, err := s.secrets.Get(ctx, s.defaultTenant(r), key)
	if err != nil {
		span.RecordError(err)
		writeSecretError(w, err)
		return
	}
	// The plaintext leaves the runtime here. We do NOT log the response
	// body; the only sink is the HTTP writer.
	writeJSON(w, http.StatusOK, secretValueResponse{
		Key:   key,
		Value: string(plaintext),
	})
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
