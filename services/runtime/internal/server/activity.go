// SPDX-License-Identifier: Apache-2.0

// User activity endpoints. These record app/product events for the
// tenant, separate from operator audit and AgentField state.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/activity"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
)

func (s *Server) registerActivityRoutes() {
	s.mux.HandleFunc("GET /api/v1/activity", s.handleListActivity)
	s.mux.HandleFunc("POST /api/v1/activity", s.handleLogActivity)
	// Operator cross-tenant view of the same log (People → Activity log).
	s.mux.HandleFunc("GET /api/v1/admin/activity",
		s.operatorGuard(rbac.ResourceAdminActivity, s.handleAdminListActivity))
}

func (s *Server) activityUnavailable(w http.ResponseWriter) bool {
	if s.activity == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("ACTIVITY_NOT_CONFIGURED",
				"user activity log is not configured on this runtime"))
		return true
	}
	return false
}

func writeActivityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, activity.ErrTenantRequired):
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED", "tenant context required", nil)
	case errors.Is(err, activity.ErrValidation):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

type logActivityInput struct {
	UserID       string         `json:"user_id,omitempty"`
	ActorType    string         `json:"actor_type,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type,omitempty"`
	ResourceID   string         `json:"resource_id,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	OccurredAt   *time.Time     `json:"occurred_at,omitempty"`
}

func (s *Server) handleLogActivity(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.activity.log")
	defer span.End()
	if s.activityUnavailable(w) {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in logActivityInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	entry, err := s.activity.Log(ctx, activity.LogInput{
		UserID:       in.UserID,
		ActorType:    activity.ActorType(strings.TrimSpace(in.ActorType)),
		Action:       in.Action,
		ResourceType: in.ResourceType,
		ResourceID:   in.ResourceID,
		Metadata:     in.Metadata,
		OccurredAt:   in.OccurredAt,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
	})
	if err != nil {
		span.RecordError(err)
		writeActivityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.activity.list")
	defer span.End()
	if s.activityUnavailable(w) {
		return
	}

	q := r.URL.Query()
	limit, offset := parsePaging(q.Get("limit"), q.Get("offset"))
	filter := activity.ListFilter{
		UserID:       strings.TrimSpace(q.Get("user_id")),
		Action:       strings.TrimSpace(q.Get("action")),
		ResourceType: strings.TrimSpace(q.Get("resource_type")),
		ResourceID:   strings.TrimSpace(q.Get("resource_id")),
		Limit:        limit,
		Offset:       offset,
	}
	if from := strings.TrimSpace(q.Get("from")); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "from must be RFC3339", nil)
			return
		}
		filter.From = &t
	}
	if to := strings.TrimSpace(q.Get("to")); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "to must be RFC3339", nil)
			return
		}
		filter.To = &t
	}

	page, err := s.activity.List(ctx, filter)
	if err != nil {
		span.RecordError(err)
		writeActivityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// handleAdminListActivity is the operator variant of handleListActivity:
// cross-tenant (RLS bypassed in the store) with an optional ?tenant=
// filter. Route is operator-gated in registerActivityRoutes.
func (s *Server) handleAdminListActivity(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.activity.admin_list")
	defer span.End()
	if s.activityUnavailable(w) {
		return
	}

	q := r.URL.Query()
	limit, offset := parsePaging(q.Get("limit"), q.Get("offset"))
	filter := activity.ListFilter{
		UserID:       strings.TrimSpace(q.Get("user_id")),
		Action:       strings.TrimSpace(q.Get("action")),
		ResourceType: strings.TrimSpace(q.Get("resource_type")),
		ResourceID:   strings.TrimSpace(q.Get("resource_id")),
		Limit:        limit,
		Offset:       offset,
	}
	if from := strings.TrimSpace(q.Get("from")); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "from must be RFC3339", nil)
			return
		}
		filter.From = &t
	}
	if to := strings.TrimSpace(q.Get("to")); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "to must be RFC3339", nil)
			return
		}
		filter.To = &t
	}

	page, err := s.activity.ListAll(ctx, strings.TrimSpace(q.Get("tenant")), filter)
	if err != nil {
		span.RecordError(err)
		writeActivityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
