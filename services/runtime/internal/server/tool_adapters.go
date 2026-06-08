// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tooladapters"
)

type toolAdapterListResponse struct {
	Adapters []tooladapters.Adapter `json:"adapters"`
}

type setToolAdapterEnabledInput struct {
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config,omitempty"`
}

type callToolInput struct {
	Adapter   string         `json:"adapter"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

func (s *Server) registerToolAdapterRoutes() {
	s.mux.HandleFunc("GET /api/v1/tools/adapters", s.handleListToolAdapters)
	s.mux.HandleFunc("PUT /api/v1/tools/adapters/{id}/enabled", s.handleSetToolAdapterEnabled)
	s.mux.HandleFunc("POST /api/v1/tools/call", s.handleCallToolAdapter)
}

func writeToolAdapterError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tooladapters.ErrTenantRequired):
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED", "tenant context required", nil)
	case errors.Is(err, tooladapters.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "TOOLS_NOT_CONFIGURED",
			"tool adapters are not configured on this runtime", nil)
	case errors.Is(err, tooladapters.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "tool adapter or tool not found", nil)
	case errors.Is(err, tooladapters.ErrDisabled):
		writeError(w, http.StatusForbidden, "TOOL_ADAPTER_DISABLED",
			"tool adapter is disabled for this tenant", nil)
	case errors.Is(err, tooladapters.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "TOOL_ADAPTER_UNAVAILABLE", err.Error(), nil)
	case errors.Is(err, tooladapters.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

func (s *Server) handleListToolAdapters(w http.ResponseWriter, r *http.Request) {
	if s.toolAdapters == nil {
		writeJSON(w, http.StatusOK, toolAdapterListResponse{Adapters: []tooladapters.Adapter{}})
		return
	}
	list, err := s.toolAdapters.List(r.Context())
	if err != nil {
		writeToolAdapterError(w, err)
		return
	}
	if list.Adapters == nil {
		list.Adapters = []tooladapters.Adapter{}
	}
	writeJSON(w, http.StatusOK, toolAdapterListResponse{Adapters: list.Adapters})
}

func (s *Server) handleSetToolAdapterEnabled(w http.ResponseWriter, r *http.Request) {
	if s.toolAdapters == nil {
		writeError(w, http.StatusServiceUnavailable, "TOOLS_NOT_CONFIGURED",
			"tool adapters are not configured on this runtime", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in setToolAdapterEnabledInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	adapter, err := s.toolAdapters.SetEnabled(
		r.Context(),
		tooladapters.AdapterID(strings.TrimSpace(r.PathValue("id"))),
		tooladapters.SetEnabledInput{Enabled: in.Enabled, Config: in.Config},
	)
	if err != nil {
		writeToolAdapterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, adapter)
}

func (s *Server) handleCallToolAdapter(w http.ResponseWriter, r *http.Request) {
	if s.toolAdapters == nil {
		writeError(w, http.StatusServiceUnavailable, "TOOLS_NOT_CONFIGURED",
			"tool adapters are not configured on this runtime", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in callToolInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	result, err := s.toolAdapters.Call(r.Context(), tooladapters.CallInput{
		Adapter:   tooladapters.AdapterID(strings.TrimSpace(in.Adapter)),
		Tool:      strings.TrimSpace(in.Tool),
		Arguments: in.Arguments,
	})
	if err != nil {
		writeToolAdapterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) registerToolAdapterOpenAPI() {
	s.openapi.Register("GET", "/api/v1/tools/adapters", openapi.RouteMeta{
		Summary: "List built-in tool adapters and tenant enablement", Tags: []string{"tools"},
	})
	s.openapi.Register("PUT", "/api/v1/tools/adapters/{id}/enabled", openapi.RouteMeta{
		Summary: "Enable or disable one built-in tool adapter", Tags: []string{"tools"},
		Parameters: []openapi.Parameter{{
			Name: "id", In: "path", Required: true,
			Schema: map[string]any{"type": "string"},
		}},
	})
	s.openapi.Register("POST", "/api/v1/tools/call", openapi.RouteMeta{
		Summary: "Call an enabled built-in tool adapter", Tags: []string{"tools"},
	})
}
