// SPDX-License-Identifier: Apache-2.0

// run_agentfield.go — #25 per-run dashboard handlers that proxy to
// AgentField.
//
// AgentField already ships a rich run / DAG / step inspector at :8081.
// Per ARCHITECTURE.md's "don't rebuild what's already excellent"
// principle, we link out for the deep view and inline a summary card +
// control actions in af-stack's run detail page.
//
// Routes (registered in server.go):
//
//	GET  /api/v1/runs/{id}/agentfield        — overview JSON + deep-link URL
//	POST /api/v1/runs/{id}/cancel            — proxy AgentField cancel
//	POST /api/v1/runs/{id}/pause             — proxy AgentField pause
//	POST /api/v1/runs/{id}/resume            — proxy AgentField resume
//	POST /api/v1/runs/{id}/request-approval  — proxy AgentField request-approval
//
// Naming note: this file lives alongside runs.go (#15 owns that one for
// the realtime subscriptions surface). Naming this `run_agentfield.go`
// keeps the two items independently mergeable while letting both wire
// into server.go's route table.

package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
)

// runAgentFieldResponse is the JSON shape returned by
// `GET /api/v1/runs/{id}/agentfield`. Field tags use snake_case to match
// the rest of the dashboard contract.
//
// `actions_available` is a server-computed allowlist of control verbs
// the dashboard's run-actions component uses to gray out buttons that
// aren't valid for the current run state (e.g. you can't resume a run
// that's already running).
type runAgentFieldResponse struct {
	Overview         agentfield.RunOverview `json:"overview"`
	AgentFieldURL    string                 `json:"agentfield_url"`
	DetailsURL       string                 `json:"details_url"`
	ActionsAvailable []string               `json:"actions_available"`
}

// agentfieldActionTimeout caps how long we wait for AgentField to ack a
// cancel / pause / resume / request-approval POST. AgentField returns
// quickly on these (sub-100ms typical) — anything past 5s indicates the
// control plane is wedged and we should fail fast so the operator gets a
// usable error.
const agentfieldActionTimeout = 5 * time.Second

// handleRunAgentField returns the AgentField summary for one run plus a
// link-out URL the dashboard can use to open AgentField's UI in a new
// tab.
//
// 503 AGENTFIELD_NOT_CONFIGURED — runtime has no AgentField client wired
// 502 AGENTFIELD_UNREACHABLE    — call to AgentField failed
// 404 RUN_NOT_FOUND             — AgentField doesn't know this run id
//
// The dashboard treats any non-2xx by rendering a graceful "AgentField
// unavailable" badge without taking the page down.
func (s *Server) handleRunAgentField(w http.ResponseWriter, r *http.Request) {
	if s.af == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("AGENTFIELD_NOT_CONFIGURED", "agentfield client not configured"))
		return
	}
	runID := r.PathValue("id")
	if !validRunID(runID) {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "invalid run id"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	overview, err := s.af.GetRunOverview(ctx, runID)
	if err != nil {
		status, code := classifyAgentFieldErr(err)
		s.log.Warn("agentfield run overview failed",
			"run_id", runID, "error", err)
		writeJSON(w, status, errEnvelope(code, err.Error()))
		return
	}

	// Pick the best execution id to drive control actions + the
	// "View in AgentField" deep link. Prefer the overview's
	// execution_id (canonical), fall back to the path run id.
	execID := overview.ExecutionID
	if execID == "" {
		execID = runID
	}

	base := s.af.AgentFieldURL()
	resp := runAgentFieldResponse{
		Overview:         overview,
		AgentFieldURL:    base,
		DetailsURL:       base + "/agent-api/executions/" + execID + "/details",
		ActionsAvailable: actionsForStatus(overview.Status),
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleRunCancel proxies POST /agent-api/executions/:id/cancel.
//
// We resolve the execution id off the run id by going through
// GetRunOverview first. Operators in the dashboard always click on a
// run row (which carries the run id); AgentField needs the execution id
// for the control verb. Resolving here keeps the dashboard API surface
// run-id-oriented and matches how the runs list addresses rows.
func (s *Server) handleRunCancel(w http.ResponseWriter, r *http.Request) {
	s.proxyAgentFieldAction(w, r, "cancel", (*agentfield.Client).CancelAgentExecution)
}

// handleRunPause proxies POST /agent-api/executions/:id/pause.
func (s *Server) handleRunPause(w http.ResponseWriter, r *http.Request) {
	s.proxyAgentFieldAction(w, r, "pause", (*agentfield.Client).PauseAgentExecution)
}

// handleRunResume proxies POST /agent-api/executions/:id/resume.
func (s *Server) handleRunResume(w http.ResponseWriter, r *http.Request) {
	s.proxyAgentFieldAction(w, r, "resume", (*agentfield.Client).ResumeAgentExecution)
}

// handleRunRequestApproval proxies POST /agent-api/executions/:id/request-approval.
func (s *Server) handleRunRequestApproval(w http.ResponseWriter, r *http.Request) {
	s.proxyAgentFieldAction(w, r, "request-approval", (*agentfield.Client).RequestAgentApproval)
}

// proxyAgentFieldAction is the shared implementation for the four
// control-verb handlers. Each handler binds one of the agentfield.Client
// methods; this fn does the run-id → execution-id resolution and the
// uniform error mapping. Pulling this out keeps the per-verb handlers
// one-liners and avoids four near-identical copies of the same code.
func (s *Server) proxyAgentFieldAction(
	w http.ResponseWriter,
	r *http.Request,
	verb string,
	call func(*agentfield.Client, context.Context, string) error,
) {
	if s.af == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("AGENTFIELD_NOT_CONFIGURED", "agentfield client not configured"))
		return
	}
	runID := r.PathValue("id")
	if !validRunID(runID) {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "invalid run id"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), agentfieldActionTimeout)
	defer cancel()

	overview, err := s.af.GetRunOverview(ctx, runID)
	if err != nil {
		status, code := classifyAgentFieldErr(err)
		s.log.Warn("agentfield run resolution failed",
			"run_id", runID, "verb", verb, "error", err)
		writeJSON(w, status, errEnvelope(code, err.Error()))
		return
	}
	execID := overview.ExecutionID
	if execID == "" {
		writeJSON(w, http.StatusBadGateway,
			errEnvelope("AGENTFIELD_NO_EXECUTION_ID",
				"agentfield returned no execution id for this run"))
		return
	}

	if err := call(s.af, ctx, execID); err != nil {
		status, code := classifyAgentFieldErr(err)
		s.log.Warn("agentfield action failed",
			"run_id", runID, "execution_id", execID, "verb", verb, "error", err)
		writeJSON(w, status, errEnvelope(code, err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"run_id":       runID,
		"execution_id": execID,
		"action":       verb,
		"status":       "ok",
	})
}

// classifyAgentFieldErr maps an agentfield client error into an HTTP
// status + machine-readable code. Keeps the handler error paths
// consistent so the dashboard branches reliably on `error.code`.
//
// The agentfield package emits string errors (no typed-error surface
// today). Using substring matching keeps the change additive vs. the
// existing client; switching to typed errors would require touching
// client.go's existing methods, which #15 also extends.
func classifyAgentFieldErr(err error) (int, string) {
	if err == nil {
		return http.StatusInternalServerError, "INTERNAL"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, "AGENTFIELD_TIMEOUT"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found"):
		return http.StatusNotFound, "RUN_NOT_FOUND"
	case strings.Contains(msg, "URL not configured"):
		return http.StatusServiceUnavailable, "AGENTFIELD_NOT_CONFIGURED"
	}
	return http.StatusBadGateway, "AGENTFIELD_UNREACHABLE"
}

// actionsForStatus returns the verbs the dashboard should expose as
// enabled for a run in the given status. AgentField's status enum isn't
// strictly typed on our side; we lowercase + match conservatively.
//
// The dashboard buttons all submit regardless; this is a UX hint, not a
// security gate. AgentField will still reject an invalid transition
// (e.g. resuming a succeeded run) — we just don't surface that as the
// happy path.
func actionsForStatus(status string) []string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running":
		return []string{"cancel", "pause", "request-approval"}
	case "paused":
		return []string{"cancel", "resume", "request-approval"}
	case "awaiting_approval", "awaiting-approval":
		return []string{"cancel", "resume"}
	case "queued", "pending":
		return []string{"cancel"}
	case "succeeded", "failed", "cancelled", "canceled", "error", "completed":
		return []string{}
	}
	// Unknown status — be permissive so the operator isn't locked out.
	return []string{"cancel", "pause", "resume", "request-approval"}
}
