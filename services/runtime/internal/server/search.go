// SPDX-License-Identifier: Apache-2.0

// App-data search endpoints. This surface indexes records owned by the
// user's product or workload modules. It deliberately does not expose
// AgentField memory, sessions, runs, spans, or traces.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/search"
)

func (s *Server) registerSearchRoutes() {
	s.mux.HandleFunc("POST /api/v1/search", s.handleSearch)
	s.mux.HandleFunc("GET /api/v1/search/indexes", s.handleSearchIndexes)
	s.mux.HandleFunc("PUT /api/v1/search/documents", s.handleUpsertSearchDocument)
	s.mux.HandleFunc("DELETE /api/v1/search/documents/{namespace}/{key}", s.handleDeleteSearchDocument)
}

func (s *Server) searchUnavailable(w http.ResponseWriter) bool {
	if s.search == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("SEARCH_NOT_CONFIGURED",
				"app-data search is not configured on this runtime"))
		return true
	}
	return false
}

func writeSearchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, search.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "search document not found", nil)
	case errors.Is(err, search.ErrTenantRequired):
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED", "tenant context required", nil)
	case errors.Is(err, search.ErrValidation):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	case errors.Is(err, search.ErrEmbedderNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "EMBEDDER_NOT_CONFIGURED",
			"vector search requires an embedder; configure the LLM gateway embedding model", nil)
	case errors.Is(err, search.ErrEmbeddingDimMismatch):
		writeError(w, http.StatusBadGateway, "EMBEDDING_DIM_MISMATCH",
			"embedder returned vector with wrong dimension", nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

type searchInput struct {
	Query          string         `json:"query"`
	Mode           string         `json:"mode,omitempty"`
	Namespace      string         `json:"namespace,omitempty"`
	MetadataFilter map[string]any `json:"metadata_filter,omitempty"`
	Limit          int            `json:"limit,omitempty"`
	Offset         int            `json:"offset,omitempty"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.search")
	defer span.End()
	if s.searchUnavailable(w) {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in searchInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}

	result, err := s.search.Search(ctx, search.Request{
		Query:          in.Query,
		Mode:           search.Mode(strings.TrimSpace(in.Mode)),
		Namespace:      in.Namespace,
		MetadataFilter: in.MetadataFilter,
		Limit:          in.Limit,
		Offset:         in.Offset,
	})
	if err != nil {
		span.RecordError(err)
		writeSearchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type upsertSearchDocumentInput struct {
	Namespace string         `json:"namespace,omitempty"`
	Key       string         `json:"key"`
	Title     string         `json:"title,omitempty"`
	Body      string         `json:"body,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Embed     bool           `json:"embed,omitempty"`
}

func (s *Server) handleUpsertSearchDocument(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.search.upsert")
	defer span.End()
	if s.searchUnavailable(w) {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in upsertSearchDocumentInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	doc, err := s.search.Upsert(ctx, search.UpsertInput{
		Namespace: in.Namespace,
		Key:       in.Key,
		Title:     in.Title,
		Body:      in.Body,
		Metadata:  in.Metadata,
		Embed:     in.Embed,
	})
	if err != nil {
		span.RecordError(err)
		writeSearchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleDeleteSearchDocument(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.search.delete")
	defer span.End()
	if s.searchUnavailable(w) {
		return
	}

	namespace := strings.TrimSpace(r.PathValue("namespace"))
	key := strings.TrimSpace(r.PathValue("key"))
	if err := s.search.Delete(ctx, search.DeleteInput{Namespace: namespace, Key: key}); err != nil {
		span.RecordError(err)
		writeSearchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

type searchIndexStatsResponse struct {
	Indexes []searchIndexStat `json:"indexes"`
}

type searchIndexStat struct {
	Schema      string  `json:"schema"`
	Table       string  `json:"table"`
	Index       string  `json:"index"`
	SizeBytes   int64   `json:"size_bytes"`
	IndexScans  int64   `json:"index_scans"`
	RowsRead    int64   `json:"rows_read"`
	RowsFetched int64   `json:"rows_fetched"`
	LastVacuum  *string `json:"last_vacuum"`
	Definition  string  `json:"definition"`
}

func (s *Server) handleSearchIndexes(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.search.indexes")
	defer span.End()
	if s.db == nil || s.db.Pool == nil {
		writeJSON(w, http.StatusOK, searchIndexStatsResponse{Indexes: []searchIndexStat{}})
		return
	}
	rows, err := s.db.Pool.Query(ctx, `
		select i.schemaname, i.relname, i.indexrelname,
		       pg_relation_size(i.indexrelid)::bigint,
		       i.idx_scan::bigint, i.idx_tup_read::bigint, i.idx_tup_fetch::bigint,
		       greatest(t.last_vacuum, t.last_autovacuum),
		       pg_get_indexdef(i.indexrelid)
		  from pg_stat_user_indexes i
		  left join pg_stat_user_tables t on t.relid = i.relid
		 where i.relname = 'suite_search_documents'
		    or i.indexrelname ilike '%search%'
		    or pg_get_indexdef(i.indexrelid) ilike '%gin%'
		 order by pg_relation_size(i.indexrelid) desc
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	defer rows.Close()
	out := []searchIndexStat{}
	for rows.Next() {
		var (
			item searchIndexStat
			last *time.Time
		)
		if err := rows.Scan(&item.Schema, &item.Table, &item.Index, &item.SizeBytes,
			&item.IndexScans, &item.RowsRead, &item.RowsFetched, &last, &item.Definition); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
			return
		}
		if last != nil {
			s := last.UTC().Format(time.RFC3339Nano)
			item.LastVacuum = &s
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, searchIndexStatsResponse{Indexes: out})
}
