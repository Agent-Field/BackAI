// SPDX-License-Identifier: Apache-2.0

// dbstudio.go — REST handlers for the DB studio (Phase 8.1).
//
// Endpoints map 1:1 to the zod schemas in apps/dashboard/src/lib/api.ts:
//
//	GET  /api/v1/db/tables                             -> DBTableListSchema
//	GET  /api/v1/db/tables/{schema}/{name}             -> DBTableDetailSchema
//	GET  /api/v1/db/rows?schema=&table=&limit=&offset= -> SQLRunResultSchema
//	POST /api/v1/db/sql                                -> SQLRunResultSchema (body SQLRunRequest)
//
// All handlers tolerate a nil dbstudio.Studio with a 503
// DB_STUDIO_NOT_CONFIGURED envelope so the runtime stays bootable
// without a database (e.g. early local dev).
//
// Authorization: these routes are dashboard-only. The tenant resolver
// (tenant_resolver.go) treats /api/v1/db/* as a public prefix — the
// dashboard's own session auth gates the operator UI, the same way
// /api/v1/admin/* works. The route is not exposed to API-key callers.
//
// The POST /api/v1/db/sql write path (readOnly=false) is reserved for
// admin callers. We thread the flag through to dbstudio.RunSQL which
// wraps the statement in a plain transaction; we DON'T yet add an
// admin-only check here because the Phase 6 admin gate isn't wired to
// arbitrary dashboard routes. Phase 8.x will revisit.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Agent-Field/backai/services/runtime/internal/dbstudio"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

// ─── Request shapes ───────────────────────────────────────────────────────

// sqlRunRequest mirrors SQLRunRequestSchema in api.ts. ReadOnly defaults
// to true (the safe path) when omitted — we use a pointer so a JSON
// `"read_only": false` is distinguishable from absence.
type sqlRunRequest struct {
	Statement string `json:"statement"`
	ReadOnly  *bool  `json:"read_only,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// dbTableListResponse matches DBTableListSchema.
type dbTableListResponse struct {
	Tables []dbstudio.DBTable `json:"tables"`
}

// ─── Routes ───────────────────────────────────────────────────────────────

// registerDBStudioRoutes wires the four /api/v1/db endpoints. Called
// from registerRoutes() in server.go.
func (s *Server) registerDBStudioRoutes() {
	s.mux.HandleFunc("GET /api/v1/db/tables", s.handleListDBTables)
	s.mux.HandleFunc("GET /api/v1/db/tables/{schema}/{name}", s.handleGetDBTable)
	s.mux.HandleFunc("GET /api/v1/db/rows", s.handleDBRows)
	s.mux.HandleFunc("POST /api/v1/db/sql", s.handleDBRunSQL)
	s.mux.HandleFunc("GET /api/v1/db/sql/history", s.handleSQLHistory)
	s.mux.HandleFunc("POST /api/v1/db/sql/history", s.handleSQLHistoryRecord)
}

// dbStudioUnavailable returns true (and writes a 503) if the Studio
// isn't wired up.
func (s *Server) dbStudioUnavailable(w http.ResponseWriter) bool {
	if s.dbStudio == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("DB_STUDIO_NOT_CONFIGURED",
				"db studio not configured (no database)"))
		return true
	}
	return false
}

// ─── GET /api/v1/db/tables ───────────────────────────────────────────────

func (s *Server) handleListDBTables(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dbstudio.list_tables")
	defer span.End()
	if s.dbStudioUnavailable(w) {
		return
	}
	tables, err := s.dbStudio.ListTables(ctx)
	if err != nil {
		span.RecordError(err)
		s.log.Error("dbstudio: list tables failed", "error", err)
		writeError(w, http.StatusInternalServerError,
			"INTERNAL", "list tables failed", nil)
		return
	}
	if tables == nil {
		tables = []dbstudio.DBTable{}
	}
	span.SetAttributes(attribute.Int("dbstudio.tables", len(tables)))
	writeJSON(w, http.StatusOK, dbTableListResponse{Tables: tables})
}

// ─── GET /api/v1/db/tables/{schema}/{name} ───────────────────────────────

func (s *Server) handleGetDBTable(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dbstudio.table")
	defer span.End()
	if s.dbStudioUnavailable(w) {
		return
	}
	schema := r.PathValue("schema")
	name := r.PathValue("name")
	span.SetAttributes(
		attribute.String("dbstudio.schema", schema),
		attribute.String("dbstudio.table", name),
	)
	detail, err := s.dbStudio.Table(ctx, schema, name)
	if err != nil {
		span.RecordError(err)
		writeDBStudioError(w, s, err, "table lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// ─── GET /api/v1/db/rows ─────────────────────────────────────────────────

func (s *Server) handleDBRows(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dbstudio.rows")
	defer span.End()
	if s.dbStudioUnavailable(w) {
		return
	}
	q := r.URL.Query()
	schema := q.Get("schema")
	if schema == "" {
		schema = "public"
	}
	table := q.Get("table")
	if table == "" {
		writeError(w, http.StatusBadRequest,
			"VALIDATION_FAILED", "table is required", nil)
		return
	}
	limit, offset := parsePaging(q.Get("limit"), q.Get("offset"))
	span.SetAttributes(
		attribute.String("dbstudio.schema", schema),
		attribute.String("dbstudio.table", table),
		attribute.Int("dbstudio.limit", limit),
		attribute.Int("dbstudio.offset", offset),
	)
	result, err := s.dbStudio.Rows(ctx, schema, table, limit, offset)
	if err != nil {
		span.RecordError(err)
		writeDBStudioError(w, s, err, "rows query failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ─── POST /api/v1/db/sql ─────────────────────────────────────────────────

func (s *Server) handleDBRunSQL(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dbstudio.sql")
	defer span.End()
	if s.dbStudioUnavailable(w) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest,
			"BAD_REQUEST", "could not read body", nil)
		return
	}
	var in sqlRunRequest
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest,
			"VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	if in.Statement == "" {
		writeError(w, http.StatusBadRequest,
			"VALIDATION_FAILED", "statement is required", nil)
		return
	}
	// read_only defaults to true (the safe path) per api.ts.
	readOnly := true
	if in.ReadOnly != nil {
		readOnly = *in.ReadOnly
	}
	span.SetAttributes(
		attribute.Bool("dbstudio.read_only", readOnly),
		attribute.Int("dbstudio.limit", in.Limit),
	)

	result, err := s.dbStudio.RunSQL(ctx, in.Statement, readOnly, in.Limit)
	if err != nil {
		span.RecordError(err)
		writeDBStudioError(w, s, err, "sql exec failed")
		return
	}
	if err := s.recordSQLHistory(r.Context(), in.Statement); err != nil {
		s.log.Warn("dbstudio: sql history record failed", "error", err)
	}
	writeJSON(w, http.StatusOK, result)
}

// ─── Error mapping ────────────────────────────────────────────────────────

// writeDBStudioError maps dbstudio sentinel errors to the canonical
// AF Stack error envelope. Anything unrecognised becomes a generic
// 500 with the supplied human message; the actual error is logged.
func writeDBStudioError(
	w http.ResponseWriter, s *Server, err error, msgIfUnknown string,
) {
	switch {
	case errors.Is(err, dbstudio.ErrInvalidIdentifier):
		writeError(w, http.StatusBadRequest,
			"INVALID_IDENTIFIER",
			"identifier must match ^[a-zA-Z_][a-zA-Z0-9_]*$",
			nil)
	case errors.Is(err, dbstudio.ErrReadOnlyViolation):
		writeError(w, http.StatusBadRequest,
			"READ_ONLY_VIOLATION",
			"statement attempted a write in a read-only transaction",
			nil)
	case errors.Is(err, dbstudio.ErrEmptyStatement):
		writeError(w, http.StatusBadRequest,
			"VALIDATION_FAILED", "statement is required", nil)
	default:
		s.log.Error("dbstudio: "+msgIfUnknown, "error", err)
		writeError(w, http.StatusInternalServerError,
			"INTERNAL", msgIfUnknown, nil)
	}
}

// ─── OpenAPI ──────────────────────────────────────────────────────────────

// registerDBStudioOpenAPI describes /api/v1/db/* routes for the
// generated spec. Called from registerRoutes() in server.go.
func (s *Server) registerDBStudioOpenAPI() {
	b := s.openapi
	b.Register("GET", "/api/v1/db/tables", openapi.RouteMeta{
		Summary: "List every user table/view/matview", Tags: []string{"db"},
	})
	b.Register("GET", "/api/v1/db/tables/{schema}/{name}", openapi.RouteMeta{
		Summary: "Get schema detail for one relation", Tags: []string{"db"},
		Parameters: []openapi.Parameter{
			{Name: "schema", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
			{Name: "name", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("GET", "/api/v1/db/rows", openapi.RouteMeta{
		Summary: "Paginated SELECT * for a single table", Tags: []string{"db"},
		Parameters: []openapi.Parameter{
			{Name: "schema", In: "query", Required: false,
				Description: "defaults to 'public'",
				Schema:      map[string]any{"type": "string"}},
			{Name: "table", In: "query", Required: true,
				Schema: map[string]any{"type": "string"}},
			{Name: "limit", In: "query", Required: false,
				Schema: map[string]any{"type": "integer"}},
			{Name: "offset", In: "query", Required: false,
				Schema: map[string]any{"type": "integer"}},
		},
	})
	b.Register("POST", "/api/v1/db/sql", openapi.RouteMeta{
		Summary: "Run a SQL statement (read-only by default)",
		Tags:    []string{"db"},
	})
	b.Register("GET", "/api/v1/db/sql/history", openapi.RouteMeta{
		Summary: "List the current operator's SQL history",
		Tags:    []string{"db"},
	})
	b.Register("POST", "/api/v1/db/sql/history", openapi.RouteMeta{
		Summary: "Record a SQL history entry",
		Tags:    []string{"db"},
	})
}
