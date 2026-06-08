// SPDX-License-Identifier: Apache-2.0

// General human approval gates. These are AF Stack business workflow rows,
// not AgentField execution approvals.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/approvals"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

type ApprovalsStore interface {
	Request(context.Context, approvals.RequestInput) (approvals.Approval, error)
	Get(context.Context, string) (approvals.Approval, error)
	List(context.Context, approvals.ListInput) (approvals.ListResult, error)
	Decide(context.Context, approvals.DecideInput) (approvals.Approval, error)
}

type requestApprovalInput struct {
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload,omitempty"`
}

type decideApprovalInput struct {
	Status       string `json:"status"`
	DecisionNote string `json:"decision_note,omitempty"`
}

func (s *Server) registerApprovalRoutes() {
	s.mux.HandleFunc("POST /api/v1/approvals", s.handleRequestApproval)
	s.mux.HandleFunc("GET /api/v1/approvals", s.handleListApprovals)
	s.mux.HandleFunc("GET /api/v1/approvals/{id}", s.handleGetApproval)
	s.mux.HandleFunc("POST /api/v1/approvals/{id}/decide", s.handleDecideApproval)
}

func (s *Server) registerApprovalOpenAPI() {
	s.openapi.AddTag("approvals", "Human approval gates for AF Stack workflows")
	s.openapi.Register("POST", "/api/v1/approvals", openapi.RouteMeta{
		Summary: "Request a human approval", Tags: []string{"approvals"},
	})
	s.openapi.Register("GET", "/api/v1/approvals", openapi.RouteMeta{
		Summary: "List approval requests", Tags: []string{"approvals"},
		Parameters: []openapi.Parameter{
			{Name: "status", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "kind", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "limit", In: "query", Schema: map[string]any{"type": "integer"}},
			{Name: "offset", In: "query", Schema: map[string]any{"type": "integer"}},
		},
	})
	s.openapi.Register("GET", "/api/v1/approvals/{id}", openapi.RouteMeta{
		Summary: "Get an approval request", Tags: []string{"approvals"},
		Parameters: []openapi.Parameter{{
			Name: "id", In: "path", Required: true,
			Schema: map[string]any{"type": "string"},
		}},
	})
	s.openapi.Register("POST", "/api/v1/approvals/{id}/decide", openapi.RouteMeta{
		Summary: "Approve, deny, or cancel an approval request", Tags: []string{"approvals"},
		Parameters: []openapi.Parameter{{
			Name: "id", In: "path", Required: true,
			Schema: map[string]any{"type": "string"},
		}},
	})
}

func (s *Server) approvalsUnavailable(w http.ResponseWriter) bool {
	if s.approvals == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("APPROVALS_NOT_CONFIGURED", "approvals store not configured"))
		return true
	}
	return false
}

func (s *Server) handleRequestApproval(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "approvals.request")
	defer span.End()
	if s.approvalsUnavailable(w) {
		return
	}
	var in requestApprovalInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
		return
	}
	a, err := s.approvals.Request(ctx, approvals.RequestInput{
		Kind:        in.Kind,
		Payload:     in.Payload,
		RequestedBy: tenantctx.UserID(ctx),
	})
	if err != nil {
		writeApprovalError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, a)
}

func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "approvals.list")
	defer span.End()
	if s.approvalsUnavailable(w) {
		return
	}
	limit, offset := parsePaging(r.URL.Query().Get("limit"), r.URL.Query().Get("offset"))
	res, err := s.approvals.List(ctx, approvals.ListInput{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Kind:   strings.TrimSpace(r.URL.Query().Get("kind")),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeApprovalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "approvals.get")
	defer span.End()
	if s.approvalsUnavailable(w) {
		return
	}
	a, err := s.approvals.Get(ctx, r.PathValue("id"))
	if err != nil {
		writeApprovalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "approvals.decide")
	defer span.End()
	if s.approvalsUnavailable(w) {
		return
	}
	var in decideApprovalInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
		return
	}
	a, err := s.approvals.Decide(ctx, approvals.DecideInput{
		ID:           r.PathValue("id"),
		Status:       in.Status,
		DecisionNote: in.DecisionNote,
		DecidedBy:    tenantctx.UserID(ctx),
	})
	if err != nil {
		writeApprovalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func writeApprovalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, approvals.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errEnvelope("NOT_FOUND", "approval request not found"))
	case errors.Is(err, approvals.ErrTenantRequired):
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED", "tenant context required", nil)
	case errors.Is(err, approvals.ErrValidation):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

func readJSON(r *http.Request, out any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return errors.New("could not read body")
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		body = []byte(`{}`)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return errors.New("invalid JSON body: " + err.Error())
	}
	return nil
}
