// SPDX-License-Identifier: Apache-2.0

// sandbox.go — REST handlers for the suite sandbox runs.
//
// Endpoints map 1:1 to SandboxRunSchema / SandboxRunListSchema /
// SandboxRunInputSchema / SandboxPoolStatsSchema /
// SandboxCapabilitiesSchema in apps/dashboard/src/lib/api.ts. Any drift
// here breaks the dashboard's safeParse — keep field names snake_case
// (the sandbox.SandboxRun struct already carries them), keep nullable
// fields emitted as JSON null (Go nil pointer), and keep arrays non-nil
// even when empty.
//
// The handlers delegate all real sandbox work to sandbox.Service, which
// wraps an adapter (Docker in v1) + a Recorder (suite_sandbox_runs).
// When the Service isn't wired (no DB / no Docker socket at boot),
// every endpoint returns 503 SANDBOX_NOT_CONFIGURED.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
)

// ─── Request body ─────────────────────────────────────────────────────────

// sandboxRunInput mirrors SandboxRunInputSchema in api.ts. Pointer
// fields stand in for "omitted" so the handler can apply the schema
// defaults rather than treat 0 as "the caller asked for 0".
type sandboxRunInput struct {
	Image       string            `json:"image"`
	Command     []string          `json:"command"`
	Files       map[string]string `json:"files,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	TimeoutS    *int              `json:"timeout_s,omitempty"`
	CPU         *int              `json:"cpu,omitempty"`
	MemoryGB    *int              `json:"memory_gb,omitempty"`
	Network     string            `json:"network,omitempty"`
	AllowEgress []string          `json:"allow_egress,omitempty"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
}

// sandboxRunListResponse mirrors SandboxRunListSchema.
type sandboxRunListResponse struct {
	Runs    []sandbox.SandboxRun `json:"runs"`
	Total   int                  `json:"total"`
	HasMore bool                 `json:"has_more"`
}

// ─── Registration ─────────────────────────────────────────────────────────

// registerSandboxRoutes wires the /api/v1/sandbox/* endpoints.
func (s *Server) registerSandboxRoutes() {
	// /api/v1/sandbox is a public prefix. POST /run executes arbitrary code
	// on the host docker daemon and was reachable unauthenticated — gate the
	// mutating routes to operators (reads stay open for the dashboard). The
	// internal agent/module path presents an operator key (AF_STACK_API_KEY).
	s.mux.HandleFunc("POST /api/v1/sandbox/run", s.operatorGuard(rbac.ResourceAdminAdapters, s.handleSandboxRun))
	s.mux.HandleFunc("GET /api/v1/sandbox/runs", s.handleSandboxListRuns)
	s.mux.HandleFunc("GET /api/v1/sandbox/runs/{id}", s.handleSandboxGetRun)
	s.mux.HandleFunc("DELETE /api/v1/sandbox/runs/{id}", s.operatorGuard(rbac.ResourceAdminAdapters, s.handleSandboxStopRun))
	s.mux.HandleFunc("GET /api/v1/sandbox/pool", s.handleSandboxPool)
	s.mux.HandleFunc("GET /api/v1/sandbox/runs/{id}/logs", s.handleSandboxLogs)
}

// sandboxUnavailable returns true (and writes a 503) if the sandbox
// Service isn't wired up. Mirrors the pattern in secrets.go.
func (s *Server) sandboxUnavailable(w http.ResponseWriter) bool {
	if s.sandbox == nil || !s.sandbox.AdapterAvailable() {
		writeError(w, http.StatusServiceUnavailable,
			"SANDBOX_NOT_CONFIGURED",
			"sandbox module is not configured on this runtime", nil)
		return true
	}
	return false
}

// writeSandboxError maps sandbox package errors to HTTP responses.
func writeSandboxError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sandbox.ErrInvalidSpec):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	case errors.Is(err, sandbox.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "sandbox run not found", nil)
	case errors.Is(err, sandbox.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable,
			"SANDBOX_NOT_CONFIGURED",
			"sandbox module is not configured on this runtime", nil)
	case errors.Is(err, sandbox.ErrAdapter):
		writeError(w, http.StatusBadGateway, "SANDBOX_ADAPTER_ERROR", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

// ─── POST /api/v1/sandbox/run ─────────────────────────────────────────────

func (s *Server) handleSandboxRun(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.sandbox.run")
	defer span.End()
	if s.sandboxUnavailable(w) {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4 MiB cap (Files are textual)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in sandboxRunInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}

	spec := sandbox.RunSpec{
		Image:       in.Image,
		Command:     in.Command,
		Files:       in.Files,
		Env:         in.Env,
		Network:     sandbox.NetworkMode(strings.ToLower(in.Network)),
		AllowEgress: in.AllowEgress,
		WorkspaceID: in.WorkspaceID,
	}
	if in.TimeoutS != nil {
		spec.TimeoutS = *in.TimeoutS
	}
	if in.CPU != nil {
		spec.CPU = *in.CPU
	}
	if in.MemoryGB != nil {
		spec.MemoryGB = *in.MemoryGB
	}
	// Tenant comes from the resolved context (Phase 6.1). When MT is
	// off this is the default tenant.
	spec.TenantID = s.defaultTenant(r)

	run, err := s.sandbox.Run(ctx, spec)
	if err != nil {
		// We may still have a partial run row from the recorder; if
		// so, return it alongside the error envelope so the dashboard
		// can render the failure. Service.Run returns the row even on
		// failure for that reason — but the HTTP contract here favours
		// a clean error response when the failure prevented an
		// attempt; otherwise we serve the row with status="failed".
		if run != nil && run.ID != "" && !errors.Is(err, sandbox.ErrInvalidSpec) && !errors.Is(err, sandbox.ErrNotConfigured) {
			writeJSON(w, http.StatusOK, run)
			return
		}
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// ─── GET /api/v1/sandbox/runs ─────────────────────────────────────────────

func (s *Server) handleSandboxListRuns(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.sandbox.list")
	defer span.End()

	resp := sandboxRunListResponse{Runs: []sandbox.SandboxRun{}}
	if s.sandbox == nil {
		// Tolerant: no service wired at all -> empty page (matches the
		// jobs handler's "no manager wired" behaviour).
		writeJSON(w, http.StatusOK, resp)
		return
	}

	q := r.URL.Query()
	limit, offset := parsePaging(q.Get("limit"), q.Get("offset"))
	filters := sandbox.ListFilters{
		TenantID: strings.TrimSpace(q.Get("tenant")),
		Status:   strings.TrimSpace(q.Get("status")),
		Limit:    limit,
		Offset:   offset,
	}
	res, err := s.sandbox.List(ctx, filters)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	resp.Total = res.Total
	resp.HasMore = res.HasMore
	if res.Runs != nil {
		resp.Runs = res.Runs
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── GET /api/v1/sandbox/runs/{id} ────────────────────────────────────────

func (s *Server) handleSandboxGetRun(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.sandbox.get")
	defer span.End()
	if s.sandbox == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "sandbox run not found", nil)
		return
	}
	id := r.PathValue("id")
	run, err := s.sandbox.Get(ctx, id)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// ─── DELETE /api/v1/sandbox/runs/{id} ─────────────────────────────────────

func (s *Server) handleSandboxStopRun(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.sandbox.stop")
	defer span.End()
	if s.sandboxUnavailable(w) {
		return
	}
	id := r.PathValue("id")
	if err := s.sandbox.Stop(ctx, id); err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"stopped": true})
}

// ─── GET /api/v1/sandbox/pool ─────────────────────────────────────────────

func (s *Server) handleSandboxPool(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.sandbox.pool")
	defer span.End()

	if s.sandbox == nil {
		// Tolerant empty pool stats so the dashboard renders the
		// "Sandbox module disabled" panel rather than 503.
		stats := sandbox.PoolStats{
			Adapter: "unconfigured",
			Capabilities: sandbox.Capabilities{
				Adapter: "unconfigured",
			},
		}
		writeJSON(w, http.StatusOK, stats)
		return
	}
	stats, err := s.sandbox.PoolStats(ctx)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// ─── GET /api/v1/sandbox/runs/{id}/logs ───────────────────────────────────

// handleSandboxLogs is a Server-Sent Events (SSE) stream of LogLines
// from the running container. The handler subscribes to the adapter's
// Stream method using the existing row's spec (image, command, env)
// reconstituted from the DB row.
//
// v1 limitation: Stream re-creates a fresh container rather than
// attaching to an already-running one. This is fine for the dashboard's
// "tail running logs" use case if the original POST /run call hasn't
// terminated yet — but the dashboard needs to know the run is still
// live. Phase 9.2 will switch to attaching to the live container by id.
func (s *Server) handleSandboxLogs(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.sandbox.logs")
	defer span.End()
	if s.sandboxUnavailable(w) {
		return
	}

	id := r.PathValue("id")
	run, err := s.sandbox.Get(ctx, id)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	if run.Status.IsTerminal() {
		// Run already finished. We don't replay stored logs over SSE
		// in v1 — the dashboard can fetch stdout_url / stderr_url
		// directly. Tell the client the stream is empty.
		writeError(w, http.StatusGone, "RUN_TERMINAL",
			"run already terminated; fetch stdout_url / stderr_url instead", nil)
		return
	}

	// Reconstruct the spec from the row. Image + command come straight
	// from the persisted columns; everything else uses defaults (the
	// stream is for tailing, not reconfiguring).
	spec := sandbox.RunSpec{
		ID:      run.ID,
		Image:   run.Image,
		Command: run.Command,
	}
	if run.TenantID != nil {
		spec.TenantID = *run.TenantID
	}
	if run.WorkspaceID != nil {
		spec.WorkspaceID = *run.WorkspaceID
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError,
			"NO_STREAMING", "response writer does not support streaming", nil)
		return
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	lines, results, err := s.sandbox.Stream(streamCtx, spec)
	if err != nil {
		// We've already set SSE headers, so write the failure as an
		// SSE event rather than swapping to a JSON envelope.
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	for {
		select {
		case <-streamCtx.Done():
			return
		case line, ok := <-lines:
			if !ok {
				lines = nil // sentinel: stop selecting on this case
				continue
			}
			payload := map[string]any{
				"ts":     line.TS.UTC().Format(time.RFC3339Nano),
				"stream": line.Stream,
				"text":   line.Text,
			}
			b, _ := json.Marshal(payload)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		case res, ok := <-results:
			if !ok || res == nil {
				return
			}
			payload := map[string]any{
				"status":     string(res.Status),
				"exit_code":  res.ExitCode,
				"duration_s": res.DurationS,
			}
			b, _ := json.Marshal(payload)
			fmt.Fprintf(w, "event: result\ndata: %s\n\n", b)
			flusher.Flush()
			return
		}
	}
}
