// SPDX-License-Identifier: Apache-2.0

// tools_native.go — HTTP layer for the strict-interface native tool
// surface (internal/tools/). Endpoints:
//
//	GET  /api/v1/tools/native                — list tool types, active
//	                                            adapter, per-tenant enable
//	POST /api/v1/tools/native/{tool}/enable  — enable/disable for tenant
//	                                            (admin only — see audit)
//	POST /api/v1/tools/native/{tool}/invoke  — direct invoke (testing /
//	                                            non-agent callers); body
//	                                            {verb, args}
//
// This is distinct from the legacy /api/v1/tools/adapters surface
// (server/tool_adapters.go) — the legacy surface owns the
// suite_tool_adapters table; this new surface owns suite_tenant_tools.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
	"github.com/Agent-Field/backai/services/runtime/internal/tools"
)

type nativeToolListResponse struct {
	Tools []tools.TenantToolStatus `json:"tools"`
}

type setNativeEnabledInput struct {
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config,omitempty"`
}

type invokeNativeInput struct {
	Verb string         `json:"verb"`
	Args map[string]any `json:"args,omitempty"`
}

type invokeNativeResponse struct {
	Tool   tools.ToolName `json:"tool"`
	Verb   string         `json:"verb"`
	Result any            `json:"result"`
}

func (s *Server) registerNativeToolRoutes() {
	s.mux.HandleFunc("GET /api/v1/tools/native", s.handleListNativeTools)
	s.mux.HandleFunc("POST /api/v1/tools/native/{tool}/enable", s.handleEnableNativeTool)
	s.mux.HandleFunc("POST /api/v1/tools/native/{tool}/invoke", s.handleInvokeNativeTool)
}

func (s *Server) registerNativeToolOpenAPI() {
	s.openapi.Register("GET", "/api/v1/tools/native", openapi.RouteMeta{
		Summary: "List native tool types and per-tenant enablement",
		Tags:    []string{"tools"},
	})
	s.openapi.Register("POST", "/api/v1/tools/native/{tool}/enable", openapi.RouteMeta{
		Summary: "Enable or disable a native tool for the current tenant",
		Tags:    []string{"tools"},
		Parameters: []openapi.Parameter{{
			Name: "tool", In: "path", Required: true,
			Schema: map[string]any{"type": "string"},
		}},
	})
	s.openapi.Register("POST", "/api/v1/tools/native/{tool}/invoke", openapi.RouteMeta{
		Summary: "Invoke a native tool verb directly (no agent involved)",
		Tags:    []string{"tools"},
		Parameters: []openapi.Parameter{{
			Name: "tool", In: "path", Required: true,
			Schema: map[string]any{"type": "string"},
		}},
	})
}

// writeNativeToolError maps the internal/tools sentinel errors to the
// AF Stack error envelope.
func writeNativeToolError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tools.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable,
			"TOOLS_NOT_CONFIGURED",
			"native tool registry is not configured on this runtime", nil)
	case errors.Is(err, tools.ErrTenantRequired):
		writeError(w, http.StatusUnauthorized,
			"TENANT_REQUIRED", "tenant context required", nil)
	case errors.Is(err, tools.ErrToolNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "native tool not found", nil)
	case errors.Is(err, tools.ErrVerbNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "tool verb not found", nil)
	case errors.Is(err, tools.ErrAdapterDisabled):
		writeError(w, http.StatusForbidden,
			"TOOL_DISABLED", "native tool is disabled for this tenant", nil)
	case errors.Is(err, tools.ErrAdapterNotConfigured):
		writeError(w, http.StatusServiceUnavailable,
			"TOOL_ADAPTER_NOT_CONFIGURED",
			"native tool adapter is not configured (env var missing)", nil)
	case errors.Is(err, tools.ErrInvalidArgs):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

func (s *Server) handleListNativeTools(w http.ResponseWriter, r *http.Request) {
	if s.toolsRegistry == nil {
		writeJSON(w, http.StatusOK, nativeToolListResponse{Tools: []tools.TenantToolStatus{}})
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	list, err := s.toolsRegistry.ListStatus(r.Context(), tenantID)
	if err != nil {
		writeNativeToolError(w, err)
		return
	}
	if list == nil {
		list = []tools.TenantToolStatus{}
	}
	writeJSON(w, http.StatusOK, nativeToolListResponse{Tools: list})
}

func (s *Server) handleEnableNativeTool(w http.ResponseWriter, r *http.Request) {
	if s.toolsRegistry == nil {
		writeError(w, http.StatusServiceUnavailable,
			"TOOLS_NOT_CONFIGURED",
			"native tool registry is not configured on this runtime", nil)
		return
	}
	name := strings.TrimSpace(r.PathValue("tool"))
	if !tools.IsValidToolName(name) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "native tool not found", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in setNativeEnabledInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED", "tenant context required", nil)
		return
	}
	status, err := s.toolsRegistry.SetEnabled(r.Context(), tenantID, tools.ToolName(name), in.Enabled, in.Config)
	if err != nil {
		writeNativeToolError(w, err)
		return
	}
	// Audit the enable/disable so the operator log shows tool-policy changes.
	if s.audit != nil {
		s.audit.Write(r.Context(), r, audit.Event{
			Action:       "native_tool.set_enabled",
			ResourceType: "native_tool",
			ResourceID:   name,
			Metadata: map[string]any{
				"enabled": in.Enabled,
			},
		})
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleInvokeNativeTool(w http.ResponseWriter, r *http.Request) {
	if s.toolsRegistry == nil {
		writeError(w, http.StatusServiceUnavailable,
			"TOOLS_NOT_CONFIGURED",
			"native tool registry is not configured on this runtime", nil)
		return
	}
	name := strings.TrimSpace(r.PathValue("tool"))
	if !tools.IsValidToolName(name) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "native tool not found", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in invokeNativeInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	verb := strings.TrimSpace(in.Verb)
	if verb == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "verb required", nil)
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	result, err := s.toolsRegistry.Invoke(r.Context(), tenantID, tools.ToolName(name), verb, in.Args)
	if err != nil {
		writeNativeToolError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, invokeNativeResponse{
		Tool:   tools.ToolName(name),
		Verb:   verb,
		Result: result,
	})
}
