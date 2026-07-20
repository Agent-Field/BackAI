// SPDX-License-Identifier: Apache-2.0

// jobs_worker.go — REST surface for the pull-based remote worker protocol
// (PRD R3). Out-of-process workers (packages/sdk-py, packages/sdk-ts) drive
// these endpoints to execute python/typescript job definitions that the Go
// runtime cannot run in-process.
//
//	POST /api/v1/jobs/worker/lease      long-poll for the next ready attempt
//	POST /api/v1/jobs/worker/heartbeat  extend a lease; learn of cancellation
//	POST /api/v1/jobs/worker/complete   report success + result
//	POST /api/v1/jobs/worker/fail       report failure (retryable or not)
//	POST /api/v1/jobs/worker/logs       attach structured log lines to a run
//
// Auth: a worker authenticates with a tenant API key carrying the
// `jobs:work` scope. Leasing is tenant-scoped — a key only ever sees its
// own tenant's attempts (enforced in-handler AND by FORCE RLS in the store).
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/jobs"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// registerJobsWorkerRoutes wires the pull-worker endpoints and their OpenAPI
// metadata. Called from server.go's route setup with a single line.
func (s *Server) registerJobsWorkerRoutes() {
	s.mux.HandleFunc("POST /api/v1/jobs/worker/lease", s.handleWorkerLease)
	s.mux.HandleFunc("POST /api/v1/jobs/worker/heartbeat", s.handleWorkerHeartbeat)
	s.mux.HandleFunc("POST /api/v1/jobs/worker/complete", s.handleWorkerComplete)
	s.mux.HandleFunc("POST /api/v1/jobs/worker/fail", s.handleWorkerFail)
	s.mux.HandleFunc("POST /api/v1/jobs/worker/logs", s.handleWorkerLogs)
	if s.openapi == nil {
		return
	}
	for path, summary := range map[string]string{
		"/api/v1/jobs/worker/lease":     "Lease the next ready remote job (long-poll)",
		"/api/v1/jobs/worker/heartbeat": "Extend a remote job lease; learn of cancellation",
		"/api/v1/jobs/worker/complete":  "Report a remote job completed",
		"/api/v1/jobs/worker/fail":      "Report a remote job failed (retryable or permanent)",
		"/api/v1/jobs/worker/logs":      "Attach structured log lines to a remote job run",
	} {
		s.openapi.Register("POST", path, openapi.RouteMeta{Summary: summary, Tags: []string{"jobs"}})
	}
}

// scopeJobsWork is the API-key scope a worker must hold to lease/execute
// remote jobs.
const scopeJobsWork = "jobs:work"

// Lease TTL / wait bounds. Workers request a TTL; we clamp it so a buggy
// client can neither hold a lease forever nor churn the queue.
const (
	defaultLeaseTTL = 30 * time.Second
	minLeaseTTL     = 5 * time.Second
	maxLeaseTTL     = 5 * time.Minute
	maxLeaseWait    = 25 * time.Second
	leasePollEvery  = 500 * time.Millisecond
)

// ─── request/response shapes (snake_case wire contract) ───────────────────

type workerLeaseRequest struct {
	Kinds           []string `json:"kinds"`
	WorkerID        string   `json:"worker_id"`
	LeaseTTLSeconds int      `json:"lease_ttl_seconds"`
	WaitSeconds     int      `json:"wait_seconds"`
}

type workerAttempt struct {
	JobID          string          `json:"job_id"`
	Attempt        int             `json:"attempt"`
	Kind           string          `json:"kind"`
	Payload        json.RawMessage `json:"payload"`
	TenantID       string          `json:"tenant_id"`
	Deadline       *string         `json:"deadline"`
	LeaseExpiresAt *string         `json:"lease_expires_at"`
}

type workerLeaseResponse struct {
	// Job is null when no attempt was available within the wait window.
	Job *workerAttempt `json:"job"`
}

type workerHeartbeatRequest struct {
	JobID           string `json:"job_id"`
	Attempt         int    `json:"attempt"`
	WorkerID        string `json:"worker_id"`
	LeaseTTLSeconds int    `json:"lease_ttl_seconds"`
}

type workerHeartbeatResponse struct {
	Canceled       bool    `json:"canceled"`
	LeaseExpiresAt *string `json:"lease_expires_at"`
}

type workerCompleteRequest struct {
	JobID    string          `json:"job_id"`
	Attempt  int             `json:"attempt"`
	WorkerID string          `json:"worker_id"`
	Result   json.RawMessage `json:"result"`
}

type workerFailRequest struct {
	JobID     string `json:"job_id"`
	Attempt   int    `json:"attempt"`
	WorkerID  string `json:"worker_id"`
	Error     string `json:"error"`
	Retryable bool   `json:"retryable"`
}

type workerLogLine struct {
	Level   string          `json:"level"`
	Message string          `json:"message"`
	Fields  json.RawMessage `json:"fields"`
	At      *string         `json:"at"`
}

type workerLogsRequest struct {
	JobID   string          `json:"job_id"`
	Attempt int             `json:"attempt"`
	Lines   []workerLogLine `json:"lines"`
}

// ─── auth ─────────────────────────────────────────────────────────────────

// jobsWorkerAuth authenticates a worker request and returns the tenant it may
// operate in. In SaaS mode the caller must present a tenant API key carrying
// the `jobs:work` scope; in personal mode (MT off) the default tenant is
// used with no scope gate. Returns ok=false after writing the error.
func (s *Server) jobsWorkerAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.jobs == nil || !s.jobs.RemoteEnabled() {
		writeError(w, http.StatusServiceUnavailable, "JOBS_NOT_CONFIGURED",
			"jobs worker protocol not available", nil)
		return "", false
	}
	ctx := r.Context()
	if !s.multiTenancyEnabled() {
		tid := tenantctx.TenantID(ctx)
		if tid == "" {
			tid = tenancy.DefaultTenantID
		}
		return tid, true
	}

	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED",
			"a jobs:work API key is required", nil)
		return "", false
	}
	if s.tenancy == nil {
		writeError(w, http.StatusServiceUnavailable, "JOBS_NOT_CONFIGURED",
			"tenancy not configured", nil)
		return "", false
	}
	k, err := s.tenancy.VerifyKey(ctx, token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_API_KEY", "invalid API key", nil)
		return "", false
	}
	// scopeSatisfied is the platform rule (scopes.go): explicit jobs:work,
	// a bare "jobs" area grant, or a full-access key (empty scopes / "*" /
	// "admin") may lease; other narrow keys may not.
	if !scopeSatisfied(k.Scopes, scopeJobsWork) {
		writeError(w, http.StatusForbidden, "SCOPE_REQUIRED",
			"API key is missing the required jobs:work scope", nil)
		return "", false
	}
	if k.TenantID == "" {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "key has no tenant", nil)
		return "", false
	}
	return k.TenantID, true
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const bearer = "Bearer "
	if len(auth) <= len(bearer) || !strings.EqualFold(auth[:len(bearer)], bearer) {
		return ""
	}
	return strings.TrimSpace(auth[len(bearer):])
}

// ─── handlers ─────────────────────────────────────────────────────────────

// handleWorkerLease long-polls for the next ready attempt.
func (s *Server) handleWorkerLease(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "jobs.worker.lease")
	defer span.End()

	tenantID, ok := s.jobsWorkerAuth(w, r)
	if !ok {
		return
	}
	var in workerLeaseRequest
	if !decodeWorkerBody(w, r, &in) {
		return
	}
	if in.WorkerID == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "worker_id is required", nil)
		return
	}
	kinds := sanitizeKinds(in.Kinds)
	if len(kinds) == 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "kinds must be a non-empty list", nil)
		return
	}
	ttl := clampTTL(in.LeaseTTLSeconds)
	wait := clampWait(in.WaitSeconds)

	deadline := time.Now().Add(wait)
	for {
		att, err := s.jobs.LeaseRemote(ctx, tenantID, kinds, in.WorkerID, ttl)
		if err != nil {
			writeWorkerStoreError(w, err)
			return
		}
		if att != nil {
			writeJSON(w, http.StatusOK, workerLeaseResponse{Job: toWorkerAttempt(att)})
			return
		}
		if !time.Now().Before(deadline) {
			writeJSON(w, http.StatusOK, workerLeaseResponse{Job: nil})
			return
		}
		select {
		case <-ctx.Done():
			writeJSON(w, http.StatusOK, workerLeaseResponse{Job: nil})
			return
		case <-time.After(leasePollEvery):
		}
	}
}

// handleWorkerHeartbeat extends a lease and reports cancellation.
func (s *Server) handleWorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "jobs.worker.heartbeat")
	defer span.End()

	tenantID, ok := s.jobsWorkerAuth(w, r)
	if !ok {
		return
	}
	var in workerHeartbeatRequest
	if !decodeWorkerBody(w, r, &in) {
		return
	}
	jobID, ok := parseJobID(w, in.JobID)
	if !ok {
		return
	}
	if in.WorkerID == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "worker_id is required", nil)
		return
	}
	ttl := clampTTL(in.LeaseTTLSeconds)
	att, err := s.jobs.HeartbeatRemote(ctx, tenantID, jobID, in.Attempt, in.WorkerID, ttl)
	if err != nil {
		writeWorkerStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workerHeartbeatResponse{
		Canceled:       att.Canceled,
		LeaseExpiresAt: rfc3339Ptr(att.LeaseExpiresAt),
	})
}

// handleWorkerComplete resolves an attempt successfully.
func (s *Server) handleWorkerComplete(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "jobs.worker.complete")
	defer span.End()

	tenantID, ok := s.jobsWorkerAuth(w, r)
	if !ok {
		return
	}
	var in workerCompleteRequest
	if !decodeWorkerBody(w, r, &in) {
		return
	}
	jobID, ok := parseJobID(w, in.JobID)
	if !ok {
		return
	}
	if in.WorkerID == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "worker_id is required", nil)
		return
	}
	if err := s.jobs.CompleteRemote(ctx, tenantID, jobID, in.Attempt, in.WorkerID, in.Result); err != nil {
		writeWorkerStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleWorkerFail resolves an attempt as failed.
func (s *Server) handleWorkerFail(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "jobs.worker.fail")
	defer span.End()

	tenantID, ok := s.jobsWorkerAuth(w, r)
	if !ok {
		return
	}
	var in workerFailRequest
	if !decodeWorkerBody(w, r, &in) {
		return
	}
	jobID, ok := parseJobID(w, in.JobID)
	if !ok {
		return
	}
	if in.WorkerID == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "worker_id is required", nil)
		return
	}
	msg := in.Error
	if strings.TrimSpace(msg) == "" {
		msg = "remote worker reported a failure"
	}
	if err := s.jobs.FailRemote(ctx, tenantID, jobID, in.Attempt, in.WorkerID, msg, in.Retryable); err != nil {
		writeWorkerStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleWorkerLogs attaches structured log lines to a run.
func (s *Server) handleWorkerLogs(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "jobs.worker.logs")
	defer span.End()

	tenantID, ok := s.jobsWorkerAuth(w, r)
	if !ok {
		return
	}
	var in workerLogsRequest
	if !decodeWorkerBody(w, r, &in) {
		return
	}
	jobID, ok := parseJobID(w, in.JobID)
	if !ok {
		return
	}
	lines := make([]jobs.LogLine, 0, len(in.Lines))
	for _, ln := range in.Lines {
		var at *time.Time
		if ln.At != nil && *ln.At != "" {
			if t, err := time.Parse(time.RFC3339, *ln.At); err == nil {
				at = &t
			}
		}
		lines = append(lines, jobs.LogLine{
			Level:   ln.Level,
			Message: ln.Message,
			Fields:  ln.Fields,
			At:      at,
		})
	}
	n, err := s.jobs.AppendRemoteLogs(ctx, tenantID, jobID, in.Attempt, lines)
	if err != nil {
		writeWorkerStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accepted": n})
}

// ─── helpers ──────────────────────────────────────────────────────────────

func decodeWorkerBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return false
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return false
	}
	return true
}

func parseJobID(w http.ResponseWriter, raw string) (int64, bool) {
	id, err := jobs.JobIDFromString(strings.TrimSpace(raw))
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "job_id must be an integer", nil)
		return 0, false
	}
	return id, true
}

func sanitizeKinds(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, k := range in {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func clampTTL(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultLeaseTTL
	}
	d := time.Duration(seconds) * time.Second
	if d < minLeaseTTL {
		return minLeaseTTL
	}
	if d > maxLeaseTTL {
		return maxLeaseTTL
	}
	return d
}

func clampWait(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	d := time.Duration(seconds) * time.Second
	if d > maxLeaseWait {
		return maxLeaseWait
	}
	return d
}

func toWorkerAttempt(a *jobs.RemoteAttempt) *workerAttempt {
	if a == nil {
		return nil
	}
	payload := a.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	return &workerAttempt{
		JobID:          jobs.JobIDToString(a.JobID),
		Attempt:        a.Attempt,
		Kind:           a.Kind,
		Payload:        payload,
		TenantID:       a.TenantID,
		Deadline:       rfc3339Ptr(a.Deadline),
		LeaseExpiresAt: rfc3339Ptr(a.LeaseExpiresAt),
	}
}

func rfc3339Ptr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

// writeWorkerStoreError maps the store's sentinel errors to the structured
// HTTP envelope.
func writeWorkerStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, jobs.ErrAttemptNotFound):
		writeError(w, http.StatusNotFound, "LEASE_NOT_FOUND", "no such lease for this tenant", nil)
	case errors.Is(err, jobs.ErrAttemptNotLeased):
		writeError(w, http.StatusConflict, "LEASE_CONFLICT", "attempt is not leased by this worker", nil)
	case errors.Is(err, jobs.ErrAttemptResolved):
		writeError(w, http.StatusConflict, "LEASE_ALREADY_RESOLVED", "attempt is already resolved", nil)
	case errors.Is(err, jobs.ErrRemoteDisabled):
		writeError(w, http.StatusServiceUnavailable, "JOBS_NOT_CONFIGURED", "jobs worker protocol not available", nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}
