// SPDX-License-Identifier: Apache-2.0

// errors_adapter.go — admin errors adapter REST surface.
package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	obserrors "github.com/Agent-Field/backai/services/runtime/internal/observability/errors"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
)

type errorGroupWire struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Service     string         `json:"service"`
	Status      string         `json:"status"`
	Count       int            `json:"count"`
	UserCount   int            `json:"user_count,omitempty"`
	FirstSeen   string         `json:"first_seen"`
	LastSeen    string         `json:"last_seen"`
	Fingerprint string         `json:"fingerprint,omitempty"`
	Permalink   string         `json:"permalink,omitempty"`
	Culprit     string         `json:"culprit,omitempty"`
	SampleEvent map[string]any `json:"sample_event,omitempty"`
}

type errorListResponse struct {
	Groups     []errorGroupWire `json:"groups"`
	Total      int              `json:"total"`
	NextCursor string           `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more,omitempty"`
}

func (s *Server) registerAdminErrorsRoutes() {
	// S1b: /api/v1/admin is on publicPrefixes (bypasses the tenant
	// resolver), so each handler must enforce operator auth itself.
	s.mux.HandleFunc("GET /api/v1/admin/errors", s.operatorGuard(rbac.ResourceAdminErrors, s.handleAdminListErrors))
	s.mux.HandleFunc("GET /api/v1/admin/errors/{id}", s.operatorGuard(rbac.ResourceAdminErrors, s.handleAdminGetError))
	s.mux.HandleFunc("GET /api/v1/admin/errors/capabilities", s.operatorGuard(rbac.ResourceAdminErrors, s.handleAdminErrorCapabilities))
	s.mux.HandleFunc("POST /api/v1/admin/errors/{id}/mute", s.operatorGuard(rbac.ResourceAdminErrors, s.handleAdminMuteError))
	s.mux.HandleFunc("POST /api/v1/admin/errors/{id}/resolve", s.operatorGuard(rbac.ResourceAdminErrors, s.handleAdminResolveError))
	s.mux.HandleFunc("POST /api/v1/admin/errors/{id}/reopen", s.operatorGuard(rbac.ResourceAdminErrors, s.handleAdminReopenError))
}

func (s *Server) handleAdminListErrors(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.errors.list")
	defer span.End()
	if s.errorsStore == nil {
		writeError(w, http.StatusServiceUnavailable, "errors_no_backend", "errors adapter is not configured", nil)
		return
	}
	filter, err := errorFilterFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
		return
	}
	page, err := s.errorsStore.ListGroups(r.Context(), filter)
	if err != nil {
		writeErrorStoreError(w, err)
		return
	}
	out := errorGroupsToWire(page.Groups)
	writeJSON(w, http.StatusOK, errorListResponse{
		Groups:     out,
		Total:      len(out),
		NextCursor: page.NextCursor,
		HasMore:    page.HasMore,
	})
}

func (s *Server) handleAdminGetError(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.errors.get")
	defer span.End()
	if s.errorsStore == nil {
		writeError(w, http.StatusServiceUnavailable, "errors_no_backend", "errors adapter is not configured", nil)
		return
	}
	groupID := strings.TrimSpace(r.PathValue("id"))
	group, err := s.errorsStore.GetGroup(r.Context(), groupID)
	if err != nil {
		writeErrorStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, errorGroupToWire(group))
}

func (s *Server) handleAdminErrorCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.errorsStore == nil {
		writeJSON(w, http.StatusOK, obserrors.Capabilities{})
		return
	}
	writeJSON(w, http.StatusOK, s.errorsStore.Capabilities())
}

func (s *Server) handleAdminMuteError(w http.ResponseWriter, r *http.Request) {
	s.updateErrorGroup(w, r, obserrors.StatusMuted, "error_group.muted")
}

func (s *Server) handleAdminResolveError(w http.ResponseWriter, r *http.Request) {
	s.updateErrorGroup(w, r, obserrors.StatusResolved, "error_group.resolved")
}

func (s *Server) handleAdminReopenError(w http.ResponseWriter, r *http.Request) {
	s.updateErrorGroup(w, r, obserrors.StatusOpen, "error_group.reopened")
}

func (s *Server) updateErrorGroup(w http.ResponseWriter, r *http.Request, status, action string) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.errors.update")
	defer span.End()
	if s.errorsStore == nil {
		writeError(w, http.StatusServiceUnavailable, "errors_no_backend", "errors adapter is not configured", nil)
		return
	}
	caps := s.errorsStore.Capabilities()
	if status == obserrors.StatusMuted && !caps.SupportsMute {
		writeError(w, http.StatusUnprocessableEntity, "unsupported_capability", "active errors adapter does not support mute", nil)
		return
	}
	if status != obserrors.StatusMuted && !caps.SupportsResolve {
		writeError(w, http.StatusUnprocessableEntity, "unsupported_capability", "active errors adapter does not support resolve/reopen", nil)
		return
	}
	groupID := strings.TrimSpace(r.PathValue("id"))
	group, err := s.errorsStore.UpdateGroup(r.Context(), groupID, obserrors.Update{Status: status})
	if err != nil {
		writeErrorStoreError(w, err)
		return
	}
	if s.audit != nil {
		s.audit.Write(r.Context(), r, audit.Event{
			Action:       action,
			ResourceType: "error_group",
			ResourceID:   groupID,
			Metadata: map[string]any{
				"status":  status,
				"adapter": s.cfg.Errors.Adapter,
			},
		})
	}
	writeJSON(w, http.StatusOK, errorGroupToWire(group))
}

func errorFilterFromRequest(r *http.Request) (obserrors.ListFilter, error) {
	q := r.URL.Query()
	status := strings.TrimSpace(q.Get("status"))
	if status == "" {
		status = obserrors.StatusOpen
	}
	if _, err := obserrors.NormalizeStatus(status); err != nil {
		return obserrors.ListFilter{}, errors.New("status must be open|muted|resolved")
	}
	filter := obserrors.ListFilter{
		Status:   status,
		Service:  strings.TrimSpace(q.Get("service")),
		TenantID: strings.TrimSpace(q.Get("tenant_id")),
		Cursor:   strings.TrimSpace(q.Get("cursor")),
	}
	if limitRaw := q.Get("limit"); limitRaw != "" {
		limit, err := strconv.Atoi(limitRaw)
		if err != nil || limit < 0 {
			return obserrors.ListFilter{}, errors.New("limit must be a positive integer")
		}
		filter.Limit = limit
	}
	if sinceRaw := firstQuery(q, "since", "from"); sinceRaw != "" {
		ts, err := time.Parse(time.RFC3339Nano, sinceRaw)
		if err != nil {
			return obserrors.ListFilter{}, errors.New("since must be RFC3339")
		}
		filter.Since = ts.UTC()
	}
	return filter, nil
}

func writeErrorStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, obserrors.ErrInvalidStatus), errors.Is(err, obserrors.ErrInvalidGroupIdentifier):
		writeError(w, http.StatusBadRequest, err.Error(), err.Error(), nil)
	case errors.Is(err, obserrors.ErrGroupNotFound):
		writeError(w, http.StatusNotFound, "error_group_not_found", "error group not found", nil)
	case errors.Is(err, obserrors.ErrUnsupportedCapability):
		writeError(w, http.StatusUnprocessableEntity, "unsupported_capability", "active errors adapter does not support this action", nil)
	case errors.Is(err, obserrors.ErrNoBackend):
		writeError(w, http.StatusServiceUnavailable, "errors_no_backend", "no errors backend configured", nil)
	case errors.Is(err, obserrors.ErrUnsupportedBackend):
		writeError(w, http.StatusUnprocessableEntity, "unsupported_errors_backend", err.Error(), nil)
	default:
		writeError(w, http.StatusBadGateway, "ERRORS_ADAPTER_FAILED", err.Error(), nil)
	}
}

func errorGroupsToWire(groups []obserrors.Group) []errorGroupWire {
	out := make([]errorGroupWire, 0, len(groups))
	for _, group := range groups {
		out = append(out, errorGroupToWire(group))
	}
	return out
}

func errorGroupToWire(group obserrors.Group) errorGroupWire {
	return errorGroupWire{
		ID:          group.ID,
		Title:       group.Title,
		Service:     defaultString(group.Service, "runtime"),
		Status:      defaultString(group.Status, obserrors.StatusOpen),
		Count:       group.Count,
		UserCount:   group.UserCount,
		FirstSeen:   timeToWire(group.FirstSeen),
		LastSeen:    timeToWire(group.LastSeen),
		Fingerprint: group.Fingerprint,
		Permalink:   group.Permalink,
		Culprit:     group.Culprit,
		SampleEvent: group.SampleEvent,
	}
}

func (s *Server) registerAdminErrorsOpenAPI() {
	params := []openapi.Parameter{
		{Name: "status", In: "query", Schema: map[string]any{"type": "string", "enum": []string{"open", "muted", "resolved"}}},
		{Name: "service", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "tenant_id", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "since", In: "query", Schema: map[string]any{"type": "string", "format": "date-time"}},
		{Name: "limit", In: "query", Schema: map[string]any{"type": "integer"}},
		{Name: "cursor", In: "query", Schema: map[string]any{"type": "string"}},
	}
	s.openapi.Register("GET", "/api/v1/admin/errors", openapi.RouteMeta{
		Summary: "List error groups through the active errors adapter", Tags: []string{"admin"}, Parameters: params,
	})
	s.openapi.Register("GET", "/api/v1/admin/errors/{id}", openapi.RouteMeta{
		Summary: "Get an error group through the active errors adapter", Tags: []string{"admin"},
		Parameters: []openapi.Parameter{{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}}},
	})
	s.openapi.Register("GET", "/api/v1/admin/errors/capabilities", openapi.RouteMeta{
		Summary: "Get active errors adapter capabilities", Tags: []string{"admin"},
	})
	for _, path := range []string{
		"/api/v1/admin/errors/{id}/mute",
		"/api/v1/admin/errors/{id}/resolve",
		"/api/v1/admin/errors/{id}/reopen",
	} {
		s.openapi.Register("POST", path, openapi.RouteMeta{
			Summary: "Update an error group through the active errors adapter",
			Tags:    []string{"admin"},
			Parameters: []openapi.Parameter{
				{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
			},
		})
	}
}
