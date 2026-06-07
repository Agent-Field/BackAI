// crons.go — REST handlers for the Phase 12.2 cron schedules surface.
//
// Endpoints map 1:1 to CronSchema / CronListSchema / CreateCronInputSchema
// in apps/dashboard/src/lib/api.ts. The handlers delegate to
// crons.Store; when the Store isn't wired (no DB at boot) reads return
// an empty page and mutations return 503 CRONS_NOT_CONFIGURED.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/crons"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

// ─── Wire shapes ──────────────────────────────────────────────────────────

// cronListResponse mirrors CronListSchema.
type cronListResponse struct {
	Crons []crons.Cron `json:"crons"`
}

// createCronInput mirrors CreateCronInputSchema.
type createCronInput struct {
	Name     string         `json:"name"`
	JobName  string         `json:"job_name"`
	Schedule string         `json:"schedule"`
	Args     map[string]any `json:"args,omitempty"`
	IsActive *bool          `json:"is_active,omitempty"`
}

// setActiveInput is the body of PUT /api/v1/crons/{id}/active.
type setActiveInput struct {
	IsActive bool `json:"is_active"`
}

// ─── Registration ─────────────────────────────────────────────────────────

func (s *Server) registerCronsRoutes() {
	s.mux.HandleFunc("GET /api/v1/crons", s.handleListCrons)
	s.mux.HandleFunc("POST /api/v1/crons", s.handleCreateCron)
	s.mux.HandleFunc("GET /api/v1/crons/{id}", s.handleGetCron)
	s.mux.HandleFunc("PUT /api/v1/crons/{id}/active", s.handleSetCronActive)
	s.mux.HandleFunc("DELETE /api/v1/crons/{id}", s.handleDeleteCron)
}

// writeCronsError maps crons package errors to HTTP responses.
func writeCronsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, crons.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	case errors.Is(err, crons.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "cron not found", nil)
	case errors.Is(err, crons.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable,
			"CRONS_NOT_CONFIGURED",
			"crons module is not configured on this runtime", nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

// ─── GET /api/v1/crons ────────────────────────────────────────────────────

func (s *Server) handleListCrons(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.crons.list")
	defer span.End()

	empty := cronListResponse{Crons: []crons.Cron{}}
	if s.crons == nil || !s.crons.HasPool() {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	q := r.URL.Query()
	filters := crons.ListFilters{TenantID: strings.TrimSpace(q.Get("tenant"))}
	out, err := s.crons.List(ctx, filters)
	if err != nil {
		writeCronsError(w, err)
		return
	}
	if out == nil {
		out = []crons.Cron{}
	}
	writeJSON(w, http.StatusOK, cronListResponse{Crons: out})
}

// ─── POST /api/v1/crons ───────────────────────────────────────────────────

func (s *Server) handleCreateCron(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.crons.create")
	defer span.End()

	if s.crons == nil || !s.crons.HasPool() {
		writeError(w, http.StatusServiceUnavailable,
			"CRONS_NOT_CONFIGURED",
			"crons module is not configured on this runtime", nil)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in createCronInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	out, err := s.crons.Create(ctx, crons.CreateInput{
		Name:     in.Name,
		JobName:  in.JobName,
		Schedule: in.Schedule,
		Args:     in.Args,
		IsActive: in.IsActive,
	})
	if err != nil {
		writeCronsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── GET /api/v1/crons/{id} ───────────────────────────────────────────────

func (s *Server) handleGetCron(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.crons.get")
	defer span.End()

	if s.crons == nil || !s.crons.HasPool() {
		writeError(w, http.StatusServiceUnavailable,
			"CRONS_NOT_CONFIGURED",
			"crons module is not configured on this runtime", nil)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "id is required", nil)
		return
	}
	out, err := s.crons.Get(ctx, id)
	if err != nil {
		writeCronsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── PUT /api/v1/crons/{id}/active ────────────────────────────────────────

func (s *Server) handleSetCronActive(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.crons.set_active")
	defer span.End()

	if s.crons == nil || !s.crons.HasPool() {
		writeError(w, http.StatusServiceUnavailable,
			"CRONS_NOT_CONFIGURED",
			"crons module is not configured on this runtime", nil)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "id is required", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in setActiveInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	out, err := s.crons.SetActive(ctx, id, in.IsActive)
	if err != nil {
		writeCronsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── DELETE /api/v1/crons/{id} ────────────────────────────────────────────

func (s *Server) handleDeleteCron(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.crons.delete")
	defer span.End()

	if s.crons == nil || !s.crons.HasPool() {
		writeError(w, http.StatusServiceUnavailable,
			"CRONS_NOT_CONFIGURED",
			"crons module is not configured on this runtime", nil)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "id is required", nil)
		return
	}
	if err := s.crons.Delete(ctx, id); err != nil {
		writeCronsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ─── OpenAPI ──────────────────────────────────────────────────────────────

func (s *Server) registerCronsOpenAPI() {
	b := s.openapi
	b.AddTag("crons", "Cron schedules over the background jobs queue")
	b.Register("GET", "/api/v1/crons", openapi.RouteMeta{
		Summary: "List cron schedules", Tags: []string{"crons"},
		Parameters: []openapi.Parameter{
			{Name: "tenant", In: "query", Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("POST", "/api/v1/crons", openapi.RouteMeta{
		Summary: "Create a cron schedule", Tags: []string{"crons"},
	})
	b.Register("GET", "/api/v1/crons/{id}", openapi.RouteMeta{
		Summary: "Get a cron schedule", Tags: []string{"crons"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("PUT", "/api/v1/crons/{id}/active", openapi.RouteMeta{
		Summary: "Toggle a cron's is_active flag", Tags: []string{"crons"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("DELETE", "/api/v1/crons/{id}", openapi.RouteMeta{
		Summary: "Delete a cron schedule", Tags: []string{"crons"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
		},
	})
}
