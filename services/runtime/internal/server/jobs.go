// SPDX-License-Identifier: Apache-2.0

// jobs.go — REST handlers for the suite jobs queue.
//
// Endpoints map 1:1 to JobSchema / JobListSchema / JobDefinitionListSchema /
// EnqueueJobInputSchema in apps/dashboard/src/lib/api.ts. Any drift here
// breaks the dashboard's safeParse — keep field names snake_case, keep
// nullable fields emitted as JSON null (Go nil pointer), and keep arrays
// non-nil even when empty.
//
// The handlers delegate all real queue work to the jobs.Manager (which
// wraps River). When the Manager isn't wired (no DB, Phase 4 boot), they
// return tolerant empty responses so the dashboard renders skeletons.
package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Agent-Field/backai/services/runtime/internal/jobs"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// ─── Shapes mirroring apps/dashboard/src/lib/api.ts ───────────────────────

// jobAttemptError matches the inner shape of JobSchema.errors[].
type jobAttemptError struct {
	At      string `json:"at"`
	Attempt int    `json:"attempt"`
	Error   string `json:"error"`
}

// jobRecord matches JobSchema. Fields with pointer types are emitted as
// JSON null per zod's nullable contract.
type jobRecord struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Args        json.RawMessage   `json:"args"`
	State       string            `json:"state"`
	TenantID    *string           `json:"tenant_id"`
	Attempt     int               `json:"attempt"`
	MaxAttempts int               `json:"max_attempts"`
	ScheduledAt string            `json:"scheduled_at"`
	EnqueuedAt  string            `json:"enqueued_at"`
	AttemptedAt *string           `json:"attempted_at"`
	FinalizedAt *string           `json:"finalized_at"`
	Errors      []jobAttemptError `json:"errors"`
}

// jobListResponse matches JobListSchema.
type jobListResponse struct {
	Jobs    []jobRecord `json:"jobs"`
	Total   int         `json:"total"`
	HasMore bool        `json:"has_more"`
}

// jobDefinitionRecent matches JobDefinitionSchema.recent.
type jobDefinitionRecent struct {
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Running   int `json:"running"`
}

// jobDefinitionRecord matches JobDefinitionSchema.
type jobDefinitionRecord struct {
	Name        string              `json:"name"`
	Language    string              `json:"language"`
	SourcePath  *string             `json:"source_path"`
	Description *string             `json:"description"`
	Cron        *string             `json:"cron"`
	Recent      jobDefinitionRecent `json:"recent"`
}

// jobDefinitionListResponse matches JobDefinitionListSchema.
type jobDefinitionListResponse struct {
	Definitions []jobDefinitionRecord `json:"definitions"`
}

// enqueueInput matches EnqueueJobInputSchema.
type enqueueInput struct {
	Name        string          `json:"name"`
	Args        json.RawMessage `json:"args"`
	ScheduledAt *string         `json:"scheduled_at,omitempty"`
	MaxAttempts *int            `json:"max_attempts,omitempty"`
}

// ─── Handlers ─────────────────────────────────────────────────────────────

// handleEnqueueJob inserts a new job.
func (s *Server) handleEnqueueJob(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.jobs.enqueue")
	defer span.End()

	if s.jobs == nil {
		writeJSON(w, http.StatusServiceUnavailable, errEnvelope("JOBS_NOT_CONFIGURED", "jobs module not configured"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errEnvelope("BAD_REQUEST", "could not read body"))
		return
	}
	var input enqueueInput
	if err := json.Unmarshal(body, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "invalid JSON body: "+err.Error()))
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "name is required"))
		return
	}
	span.SetAttributes(attribute.String("job.name", input.Name))

	opts := jobs.EnqueueOpts{}
	if input.ScheduledAt != nil && *input.ScheduledAt != "" {
		t, err := time.Parse(time.RFC3339, *input.ScheduledAt)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "scheduled_at must be RFC3339"))
			return
		}
		opts.ScheduledAt = t
	}
	if input.MaxAttempts != nil {
		opts.MaxAttempts = *input.MaxAttempts
	}

	row, err := s.jobs.Enqueue(ctx, input.Name, input.Args, opts)
	if err != nil {
		span.RecordError(err)
		writeJSON(w, http.StatusBadRequest, errEnvelope("ENQUEUE_FAILED", err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, marshalJobRow(row))
}

// handleListJobs returns a filtered page of jobs.
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.jobs.list")
	defer span.End()

	q := r.URL.Query()
	limit, offset := parsePaging(q.Get("limit"), q.Get("offset"))
	name := strings.TrimSpace(q.Get("name"))
	state := strings.TrimSpace(q.Get("state"))
	tenant := strings.TrimSpace(q.Get("tenant"))
	// S1b: the ?tenant= filter is caller-controlled. Only operators may
	// list across tenants; tenant-authenticated callers are pinned to
	// their own tenant regardless of the filter they sent.
	if s.multiTenancyEnabled() && !s.callerIsOperator(r) {
		tenant = tenantctx.TenantID(ctx)
	}

	span.SetAttributes(
		attribute.Int("paging.limit", limit),
		attribute.Int("paging.offset", offset),
		attribute.String("filter.name", name),
		attribute.String("filter.state", state),
		attribute.String("filter.tenant", tenant),
	)

	resp := jobListResponse{Jobs: []jobRecord{}}
	if s.jobs == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	res, err := s.jobs.List(ctx, jobs.ListFilters{
		Name:     name,
		State:    state,
		TenantID: tenant,
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		span.RecordError(err)
		s.log.Error("list jobs failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	resp.Total = res.Total
	resp.HasMore = res.HasMore
	for _, row := range res.Jobs {
		resp.Jobs = append(resp.Jobs, marshalJobRow(row))
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetJob returns a single job by id.
func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.jobs.get")
	defer span.End()

	if s.jobs == nil {
		writeJSON(w, http.StatusServiceUnavailable, errEnvelope("JOBS_NOT_CONFIGURED", "jobs module not configured"))
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "id must be an integer"))
		return
	}
	row, err := s.jobs.Get(ctx, id)
	if err != nil {
		span.RecordError(err)
		writeJSON(w, http.StatusNotFound, errEnvelope("NOT_FOUND", err.Error()))
		return
	}
	rec := marshalJobRow(row)
	if jobHiddenFromCaller(s, r, rec) {
		writeJSON(w, http.StatusNotFound, errEnvelope("NOT_FOUND", "job not found"))
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// jobHiddenFromCaller enforces per-tenant visibility on single-job reads:
// non-operator callers only see jobs whose args carry their own tenant id.
func jobHiddenFromCaller(s *Server, r *http.Request, rec jobRecord) bool {
	if !s.multiTenancyEnabled() || s.callerIsOperator(r) {
		return false
	}
	caller := tenantctx.TenantID(r.Context())
	return rec.TenantID == nil || caller == "" || *rec.TenantID != caller
}

// handleRetryJob marks a job retryable.
func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.jobs.retry")
	defer span.End()

	if s.jobs == nil {
		writeJSON(w, http.StatusServiceUnavailable, errEnvelope("JOBS_NOT_CONFIGURED", "jobs module not configured"))
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "id must be an integer"))
		return
	}
	if existing, err := s.jobs.Get(ctx, id); err != nil {
		span.RecordError(err)
		writeJSON(w, http.StatusNotFound, errEnvelope("NOT_FOUND", err.Error()))
		return
	} else if jobHiddenFromCaller(s, r, marshalJobRow(existing)) {
		writeJSON(w, http.StatusNotFound, errEnvelope("NOT_FOUND", "job not found"))
		return
	}
	row, err := s.jobs.Retry(ctx, id)
	if err != nil {
		span.RecordError(err)
		writeJSON(w, http.StatusBadRequest, errEnvelope("RETRY_FAILED", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, marshalJobRow(row))
}

// handleJobDefinitions lists all known kinds with their stats.
func (s *Server) handleJobDefinitions(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.jobs.definitions")
	defer span.End()

	resp := jobDefinitionListResponse{Definitions: []jobDefinitionRecord{}}
	if s.jobs == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	defs := s.jobs.Registry().All()
	for _, d := range defs {
		rec := jobDefinitionRecord{
			Name:     d.Name,
			Language: string(d.Language),
			Recent:   jobDefinitionRecent{},
		}
		if d.SourcePath != "" {
			rec.SourcePath = strPtr(d.SourcePath)
		}
		if d.Description != "" {
			rec.Description = strPtr(d.Description)
		}
		if d.Cron != "" {
			rec.Cron = strPtr(d.Cron)
		}
		stats, err := s.jobs.Registry().RecentStatsFor(ctx, s.jobs, d.Name)
		if err == nil {
			rec.Recent = jobDefinitionRecent{
				Succeeded: stats.Succeeded,
				Failed:    stats.Failed,
				Running:   stats.Running,
			}
		} else {
			s.log.Debug("recent stats failed", "kind", d.Name, "error", err)
		}
		resp.Definitions = append(resp.Definitions, rec)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// marshalJobRow converts a *rivertype.JobRow into the wire shape used by
// the dashboard. Returns the zero value for nil input.
func marshalJobRow(row *rivertype.JobRow) jobRecord {
	if row == nil {
		return jobRecord{Errors: []jobAttemptError{}}
	}
	args := extractPayload(row.EncodedArgs)
	tenantID := extractTenantID(row.EncodedArgs)

	rec := jobRecord{
		ID:          strconv.FormatInt(row.ID, 10),
		Name:        row.Kind,
		Args:        args,
		State:       string(row.State),
		Attempt:     row.Attempt,
		MaxAttempts: row.MaxAttempts,
		ScheduledAt: row.ScheduledAt.UTC().Format(time.RFC3339Nano),
		EnqueuedAt:  row.CreatedAt.UTC().Format(time.RFC3339Nano),
		Errors:      nil,
	}
	if tenantID != "" {
		rec.TenantID = strPtr(tenantID)
	}
	if row.AttemptedAt != nil {
		s := row.AttemptedAt.UTC().Format(time.RFC3339Nano)
		rec.AttemptedAt = &s
	}
	if row.FinalizedAt != nil {
		s := row.FinalizedAt.UTC().Format(time.RFC3339Nano)
		rec.FinalizedAt = &s
	}
	if len(row.Errors) > 0 {
		rec.Errors = make([]jobAttemptError, 0, len(row.Errors))
		for _, e := range row.Errors {
			rec.Errors = append(rec.Errors, jobAttemptError{
				At:      e.At.UTC().Format(time.RFC3339Nano),
				Attempt: e.Attempt,
				Error:   e.Error,
			})
		}
	}
	return rec
}

// extractPayload pulls the user-supplied JSON out of River's persisted
// encoded args. Our internal envelope is {"payload": ..., "tenant_id": ...}.
func extractPayload(encoded []byte) json.RawMessage {
	if len(encoded) == 0 {
		return json.RawMessage("null")
	}
	var aux struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(encoded, &aux); err != nil {
		return json.RawMessage(encoded)
	}
	if len(aux.Payload) == 0 {
		return json.RawMessage("null")
	}
	return aux.Payload
}

func extractTenantID(encoded []byte) string {
	if len(encoded) == 0 {
		return ""
	}
	var aux struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal(encoded, &aux); err != nil {
		return ""
	}
	return aux.TenantID
}

func strPtr(s string) *string { return &s }