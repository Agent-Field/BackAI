// SPDX-License-Identifier: Apache-2.0

// Shipwright turns a customer coding request into an AgentField coding-agent
// execution. AF Stack stores only task/patch metadata; AgentField owns the
// AI-stateful run, harness tool calls, logs, spans, traces, and memory.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/shipwright"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

const (
	defaultShipwrightAgentCall = "shipwright.build"
	envShipwrightAgentCall     = "AF_STACK_SHIPWRIGHT_AGENT_CALL"
)

type ShipwrightStore interface {
	CreateTask(context.Context, shipwright.CreateTaskInput) (shipwright.Task, error)
	StartTask(context.Context, string, string) (shipwright.Task, error)
	MarkTaskFailed(context.Context, string) (shipwright.Task, error)
	GetTask(context.Context, string) (shipwright.Task, error)
	ListTasks(context.Context, shipwright.ListTasksInput) (shipwright.ListTasksResult, error)
	CompleteTask(context.Context, shipwright.CompleteTaskInput) (shipwright.Task, error)
	ListPatches(context.Context, string) ([]shipwright.Patch, error)
}

type createShipwrightTaskInput struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	RepoURL         string `json:"repo_url"`
	HarnessProvider string `json:"harness_provider,omitempty"`
	Model           string `json:"model,omitempty"`
}

type completeShipwrightTaskInput struct {
	Status  string `json:"status,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Summary string `json:"summary,omitempty"`
	DiffURL string `json:"diff_url,omitempty"`
}

type shipwrightTaskResponse struct {
	Task          shipwright.Task    `json:"task"`
	Patches       []shipwright.Patch `json:"patches,omitempty"`
	AgentCall     string             `json:"agent_call,omitempty"`
	AgentFieldURL string             `json:"agentfield_url,omitempty"`
	DetailsURL    string             `json:"details_url,omitempty"`
}

type shipwrightTaskListResponse struct {
	Tasks   []shipwright.Task `json:"tasks"`
	Total   int               `json:"total"`
	HasMore bool              `json:"has_more"`
}

func (s *Server) registerShipwrightRoutes() {
	s.mux.HandleFunc("POST /api/v1/shipwright/tasks", s.handleCreateShipwrightTask)
	s.mux.HandleFunc("GET /api/v1/shipwright/tasks", s.handleListShipwrightTasks)
	s.mux.HandleFunc("GET /api/v1/shipwright/tasks/{id}", s.handleGetShipwrightTask)
	s.mux.HandleFunc("POST /api/v1/shipwright/tasks/{id}/complete", s.handleCompleteShipwrightTask)
}

func (s *Server) registerShipwrightOpenAPI() {
	s.openapi.AddTag("shipwright", "Autonomous coding-agent task factory")
	s.openapi.Register("POST", "/api/v1/shipwright/tasks", openapi.RouteMeta{
		Summary: "Create a Shipwright coding task and start AgentField execution",
		Tags:    []string{"shipwright"},
	})
	s.openapi.Register("GET", "/api/v1/shipwright/tasks", openapi.RouteMeta{
		Summary: "List Shipwright coding tasks",
		Tags:    []string{"shipwright"},
	})
	s.openapi.Register("GET", "/api/v1/shipwright/tasks/{id}", openapi.RouteMeta{
		Summary: "Get a Shipwright coding task",
		Tags:    []string{"shipwright"},
		Parameters: []openapi.Parameter{{
			Name: "id", In: "path", Required: true,
			Schema: map[string]any{"type": "string"},
		}},
	})
	s.openapi.Register("POST", "/api/v1/shipwright/tasks/{id}/complete", openapi.RouteMeta{
		Summary: "Record Shipwright completion metadata and optional patch pointer",
		Tags:    []string{"shipwright"},
		Parameters: []openapi.Parameter{{
			Name: "id", In: "path", Required: true,
			Schema: map[string]any{"type": "string"},
		}},
	})
}

func (s *Server) shipwrightUnavailable(w http.ResponseWriter) bool {
	if s.shipwright == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("SHIPWRIGHT_NOT_CONFIGURED", "shipwright store not configured"))
		return true
	}
	return false
}

func (s *Server) handleCreateShipwrightTask(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx, span := s.dashTracer().Start(r.Context(), "shipwright.tasks.create")
	defer span.End()

	if s.shipwrightUnavailable(w) {
		return
	}
	if s.af == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("AGENTFIELD_NOT_CONFIGURED", "agentfield client not configured"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errEnvelope("BAD_REQUEST", "could not read body"))
		return
	}
	var in createShipwrightTaskInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "invalid JSON body: "+err.Error()))
		return
	}

	task, err := s.shipwright.CreateTask(ctx, shipwright.CreateTaskInput{
		Title:       in.Title,
		Description: in.Description,
		RepoURL:     in.RepoURL,
		UserID:      tenantctx.UserID(r.Context()),
	})
	if err != nil {
		writeShipwrightError(w, err)
		return
	}

	agentCall := shipwrightAgentCall()
	afBody, err := json.Marshal(map[string]any{
		"input": map[string]any{
			"task_id":          task.ID,
			"tenant_id":        task.TenantID,
			"user_id":          task.UserID,
			"title":            task.Title,
			"description":      task.Description,
			"repo_url":         task.RepoURL,
			"harness_provider": strings.TrimSpace(in.HarnessProvider),
			"model":            strings.TrimSpace(in.Model),
			"callback_url":     "/api/v1/shipwright/tasks/" + task.ID + "/complete",
		},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}

	endpoint := "/api/v1/execute/async/" + agentCall
	afResp, err := s.af.Execute(ctx, endpoint, afBody)
	if err != nil {
		s.logGatewayRequest(r, endpoint, http.StatusBadGateway, len(afBody), 0, "", start)
		_, _ = s.shipwright.MarkTaskFailed(ctx, task.ID)
		writeJSON(w, http.StatusBadGateway, errEnvelope("AGENTFIELD_UNREACHABLE", err.Error()))
		return
	}
	execID := executionIDFromAgentField(afResp)
	if afResp.StatusCode >= 400 || execID == "" {
		s.logGatewayRequest(r, endpoint, afResp.StatusCode, len(afBody), len(afResp.Body), execID, start)
		_, _ = s.shipwright.MarkTaskFailed(ctx, task.ID)
		msg := strings.TrimSpace(string(afResp.Body))
		if msg == "" {
			msg = "agentfield did not return an execution id"
		}
		writeJSON(w, http.StatusBadGateway, errEnvelope("AGENTFIELD_EXECUTION_FAILED", msg))
		return
	}
	s.logGatewayRequest(r, endpoint, afResp.StatusCode, len(afBody), len(afResp.Body), execID, start)

	task, err = s.shipwright.StartTask(ctx, task.ID, execID)
	if err != nil {
		writeShipwrightError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, s.shipwrightResponse(task, nil, agentCall))
}

func (s *Server) handleListShipwrightTasks(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "shipwright.tasks.list")
	defer span.End()

	if s.shipwrightUnavailable(w) {
		return
	}
	limit, offset := parsePaging(r.URL.Query().Get("limit"), r.URL.Query().Get("offset"))
	res, err := s.shipwright.ListTasks(ctx, shipwright.ListTasksInput{
		Status: r.URL.Query().Get("status"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeShipwrightError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, shipwrightTaskListResponse{
		Tasks:   res.Tasks,
		Total:   res.Total,
		HasMore: res.HasMore,
	})
}

func (s *Server) handleGetShipwrightTask(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "shipwright.tasks.get")
	defer span.End()

	if s.shipwrightUnavailable(w) {
		return
	}
	task, err := s.shipwright.GetTask(ctx, r.PathValue("id"))
	if err != nil {
		writeShipwrightError(w, err)
		return
	}
	patches, err := s.shipwright.ListPatches(ctx, task.ID)
	if err != nil {
		writeShipwrightError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.shipwrightResponse(task, patches, shipwrightAgentCall()))
}

func (s *Server) handleCompleteShipwrightTask(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "shipwright.tasks.complete")
	defer span.End()

	if s.shipwrightUnavailable(w) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errEnvelope("BAD_REQUEST", "could not read body"))
		return
	}
	var in completeShipwrightTaskInput
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &in); err != nil {
			writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "invalid JSON body: "+err.Error()))
			return
		}
	}
	task, err := s.shipwright.CompleteTask(ctx, shipwright.CompleteTaskInput{
		TaskID:  r.PathValue("id"),
		Status:  in.Status,
		Ref:     in.Ref,
		Summary: in.Summary,
		DiffURL: in.DiffURL,
	})
	if err != nil {
		writeShipwrightError(w, err)
		return
	}
	patches, err := s.shipwright.ListPatches(ctx, task.ID)
	if err != nil {
		writeShipwrightError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.shipwrightResponse(task, patches, shipwrightAgentCall()))
}

func (s *Server) shipwrightResponse(task shipwright.Task, patches []shipwright.Patch, agentCall string) shipwrightTaskResponse {
	resp := shipwrightTaskResponse{
		Task:      task,
		Patches:   patches,
		AgentCall: agentCall,
	}
	if s.af != nil {
		resp.AgentFieldURL = s.af.AgentFieldURL()
		if task.RunID != nil && *task.RunID != "" {
			resp.DetailsURL = strings.TrimRight(resp.AgentFieldURL, "/") + "/agent-api/executions/" + *task.RunID + "/details"
		}
	}
	return resp
}

func writeShipwrightError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, shipwright.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errEnvelope("NOT_FOUND", "shipwright task not found"))
	case errors.Is(err, shipwright.ErrTenantRequired):
		writeJSON(w, http.StatusUnauthorized, errEnvelope("TENANT_REQUIRED", "tenant context required"))
	case errors.Is(err, shipwright.ErrValidation):
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", err.Error()))
	default:
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
	}
}

func shipwrightAgentCall() string {
	if v := strings.TrimSpace(os.Getenv(envShipwrightAgentCall)); v != "" {
		return v
	}
	return defaultShipwrightAgentCall
}

func executionIDFromAgentField(resp agentfield.ExecuteResponse) string {
	if resp.ExecutionID != "" {
		return resp.ExecutionID
	}
	var raw map[string]any
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return ""
	}
	for _, key := range []string{"execution_id", "id", "run_id"} {
		if v, ok := raw[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if out, ok := raw["output"].(map[string]any); ok {
		for _, key := range []string{"execution_id", "id", "run_id"} {
			if v, ok := out[key].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}
