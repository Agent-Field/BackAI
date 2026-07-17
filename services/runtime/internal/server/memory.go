// SPDX-License-Identifier: Apache-2.0

// Package server — memory.go wires the suite memory primitives to the
// REST endpoints consumed by the dashboard and SDKs.
//
// Every response shape must match the zod schemas in
// apps/dashboard/src/lib/api.ts exactly:
//
//	GET    /api/v1/memory                    -> MemoryListSchema
//	GET    /api/v1/memory/get?scope=&key=    -> MemoryEntrySchema
//	PUT    /api/v1/memory                    -> MemoryEntrySchema (body: MemoryPutInput)
//	DELETE /api/v1/memory?scope=&key=        -> {"deleted": true}
//	POST   /api/v1/memory/search             -> MemorySearchResultSchema (body: MemorySearchRequest)
//
// The store auto-fills scope_id from the tenant context for
// scope="tenant" so single-tenant runtimes don't have to thread the
// id through every request.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/memory"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
)

// registerMemoryRoutes wires the /api/v1/memory/* endpoints.
func (s *Server) registerMemoryRoutes() {
	// The bare list endpoint enumerates every memory row for a scope and,
	// with no scope_id, falls back to a scope-only (all-tenants) filter —
	// i.e. it can dump other tenants' memory. It lives on the /api/v1/memory
	// public prefix (tenant resolver bypassed), so gate the cross-tenant
	// browse behind operator auth like the other dashboard-owned surfaces.
	// (Scoped get/put/search remain reachable for the header-only internal
	// agent path; full closure of that path is tracked as a follow-up.)
	s.mux.HandleFunc("GET /api/v1/memory", s.operatorGuard(rbac.ResourceAdminActivity, s.handleListMemory))
	s.mux.HandleFunc("GET /api/v1/memory/get", s.handleGetMemory)
	s.mux.HandleFunc("PUT /api/v1/memory", s.handlePutMemory)
	s.mux.HandleFunc("DELETE /api/v1/memory", s.handleDeleteMemory)
	s.mux.HandleFunc("POST /api/v1/memory/search", s.handleSearchMemory)
}

// memoryUnavailable returns true (and writes a 503) when no memory
// store is wired. Mirrors vaultUnavailable / jobs handling.
func (s *Server) memoryUnavailable(w http.ResponseWriter) bool {
	if s.memory == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("MEMORY_NOT_CONFIGURED",
				"suite memory is not configured on this runtime"))
		return true
	}
	return false
}

// writeMemoryError maps memory package errors to HTTP responses.
func writeMemoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, memory.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "memory entry not found", nil)
	case errors.Is(err, memory.ErrInvalidScope):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	case errors.Is(err, memory.ErrInvalidKey):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid memory key", nil)
	case errors.Is(err, memory.ErrEmbeddingDimMismatch):
		writeError(w, http.StatusBadGateway, "EMBEDDING_DIM_MISMATCH",
			"embedder returned vector with wrong dimension", nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

// ─── GET /api/v1/memory ──────────────────────────────────────────────────

func (s *Server) handleListMemory(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.memory.list")
	defer span.End()
	if s.memoryUnavailable(w) {
		return
	}

	q := r.URL.Query()
	limit, offset := parsePaging(q.Get("limit"), q.Get("offset"))

	opts := memory.ListOpts{
		Scope:   memory.Scope(strings.TrimSpace(q.Get("scope"))),
		ScopeID: strings.TrimSpace(q.Get("scope_id")),
		Prefix:  q.Get("prefix"),
		Limit:   limit,
		Offset:  offset,
	}

	out, err := s.memory.List(ctx, opts)
	if err != nil {
		span.RecordError(err)
		writeMemoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── GET /api/v1/memory/get ──────────────────────────────────────────────

func (s *Server) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.memory.get")
	defer span.End()
	if s.memoryUnavailable(w) {
		return
	}

	q := r.URL.Query()
	scope := memory.Scope(strings.TrimSpace(q.Get("scope")))
	key := strings.TrimSpace(q.Get("key"))
	scopeID := strings.TrimSpace(q.Get("scope_id"))

	if scope == "" || key == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"scope and key query parameters are required", nil)
		return
	}

	entry, err := s.memory.Get(ctx, scope, scopeID, key)
	if err != nil {
		span.RecordError(err)
		writeMemoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

// ─── PUT /api/v1/memory ──────────────────────────────────────────────────

// putMemoryInput mirrors MemoryPutInputSchema in api.ts.
type putMemoryInput struct {
	Scope    string          `json:"scope"`
	ScopeID  string          `json:"scope_id,omitempty"`
	Key      string          `json:"key"`
	Value    json.RawMessage `json:"value"`
	Metadata map[string]any  `json:"metadata,omitempty"`
	Embed    bool            `json:"embed,omitempty"`
}

func (s *Server) handlePutMemory(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.memory.put")
	defer span.End()
	if s.memoryUnavailable(w) {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in putMemoryInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	if strings.TrimSpace(in.Scope) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "scope is required", nil)
		return
	}
	if strings.TrimSpace(in.Key) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "key is required", nil)
		return
	}

	entry, err := s.memory.Put(ctx, memory.PutInput{
		Scope:    memory.Scope(in.Scope),
		ScopeID:  in.ScopeID,
		Key:      in.Key,
		Value:    in.Value,
		Metadata: in.Metadata,
		Embed:    in.Embed,
	})
	if err != nil {
		span.RecordError(err)
		writeMemoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

// ─── DELETE /api/v1/memory ───────────────────────────────────────────────

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.memory.delete")
	defer span.End()
	if s.memoryUnavailable(w) {
		return
	}

	q := r.URL.Query()
	scope := memory.Scope(strings.TrimSpace(q.Get("scope")))
	key := strings.TrimSpace(q.Get("key"))
	scopeID := strings.TrimSpace(q.Get("scope_id"))

	if scope == "" || key == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"scope and key query parameters are required", nil)
		return
	}

	if err := s.memory.Delete(ctx, scope, scopeID, key); err != nil {
		span.RecordError(err)
		writeMemoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ─── POST /api/v1/memory/search ──────────────────────────────────────────

// searchMemoryInput mirrors MemorySearchRequestSchema.
type searchMemoryInput struct {
	Query     string   `json:"query"`
	Scope     string   `json:"scope,omitempty"`
	ScopeID   string   `json:"scope_id,omitempty"`
	TopK      int      `json:"top_k,omitempty"`
	Threshold *float64 `json:"threshold,omitempty"`
}

func (s *Server) handleSearchMemory(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.memory.search")
	defer span.End()
	if s.memoryUnavailable(w) {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in searchMemoryInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	if strings.TrimSpace(in.Query) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "query is required", nil)
		return
	}

	req := memory.SearchRequest{
		Query:     in.Query,
		Scope:     memory.Scope(in.Scope),
		ScopeID:   in.ScopeID,
		TopK:      in.TopK,
		Threshold: in.Threshold,
	}

	result, err := s.memory.Search(ctx, req)
	if err != nil {
		span.RecordError(err)
		// Search without an embedder returns a plain error; surface
		// it as a 503 with a clear code so the caller knows to wire
		// an embedder rather than retry.
		if !s.memory.HasEmbedder() {
			writeError(w, http.StatusServiceUnavailable, "EMBEDDER_NOT_CONFIGURED",
				"memory search requires an embedder; none is wired", nil)
			return
		}
		writeMemoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
