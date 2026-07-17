// SPDX-License-Identifier: Apache-2.0

// mcp.go — REST handlers for the Phase 11.1 MCP runtime.
//
// Endpoints (mirror api.ts MCP block):
//
//	GET    /api/v1/mcp/servers              -> MCPServerListSchema
//	GET    /api/v1/mcp/servers/{name}       -> MCPServerSchema
//	POST   /api/v1/mcp/servers              -> MCPServerSchema (body: CreateMCPServerInput)
//	DELETE /api/v1/mcp/servers/{name}       -> {"deleted": true}
//	PUT    /api/v1/mcp/servers/{name}/enabled -> MCPServerSchema (body: {enabled})
//	GET    /api/v1/mcp/tools?server=<name>  -> MCPToolListSchema
//	POST   /api/v1/mcp/call                 -> CallMCPResultSchema (body: CallMCPInput)
//
// When the pool isn't wired (no DB at boot) GET endpoints return
// tolerant empty pages; mutating endpoints return 503 MCP_NOT_CONFIGURED.
//
// Tenant filter: every read pulls the caller's tenant id from
// tenantctx and Pool.Servers/Tools/Call apply the per-row visibility
// rule (NULL row.tenant_id OR row.tenant_id == caller).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/mcp"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
	"github.com/Agent-Field/backai/services/runtime/internal/tools"
)

// ─── Wire shapes ──────────────────────────────────────────────────────────

// createMCPServerInput mirrors CreateMCPServerInputSchema in api.ts.
// All non-name fields are optional; the Store applies defaults.
type createMCPServerInput struct {
	Name        string            `json:"name"`
	Transport   string            `json:"transport"`
	Command     []string          `json:"command,omitempty"`
	URL         string            `json:"url,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Description string            `json:"description,omitempty"`
	TenantID    string            `json:"tenant_id,omitempty"`
}

// mcpServerListResponse mirrors MCPServerListSchema.
type mcpServerListResponse struct {
	Servers []mcp.Server `json:"servers"`
}

// mcpToolListResponse mirrors MCPToolListSchema.
type mcpToolListResponse struct {
	Tools []mcp.Tool `json:"tools"`
}

// setEnabledInput is the body of PUT /api/v1/mcp/servers/{name}/enabled.
type setEnabledInput struct {
	Enabled bool `json:"enabled"`
}

// callMCPInput mirrors CallMCPInputSchema.
type callMCPInput struct {
	Server    string         `json:"server"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ─── Registration ─────────────────────────────────────────────────────────

func (s *Server) registerMCPRoutes() {
	// The /api/v1/mcp surface is a public prefix (tenant resolver bypassed).
	// Reads (list servers/tools) stay open for the dashboard, but the
	// privileged routes were unauthenticated: POST /call runs native tools
	// (native:exec = sandbox code exec, native:sql = DB read) and the server
	// CRUD lets anyone register a global MCP/SSE server every tenant then
	// trusts. Gate those to operators.
	s.mux.HandleFunc("GET /api/v1/mcp/servers", s.handleListMCPServers)
	s.mux.HandleFunc("GET /api/v1/mcp/servers/{name}", s.handleGetMCPServer)
	s.mux.HandleFunc("POST /api/v1/mcp/servers", s.operatorGuard(rbac.ResourceAdminAdapters, s.handleAddMCPServer))
	s.mux.HandleFunc("DELETE /api/v1/mcp/servers/{name}", s.operatorGuard(rbac.ResourceAdminAdapters, s.handleRemoveMCPServer))
	s.mux.HandleFunc("PUT /api/v1/mcp/servers/{name}/enabled", s.operatorGuard(rbac.ResourceAdminAdapters, s.handleEnableMCPServer))
	s.mux.HandleFunc("GET /api/v1/mcp/tools", s.handleListMCPTools)
	s.mux.HandleFunc("POST /api/v1/mcp/call", s.operatorGuard(rbac.ResourceAdminAdapters, s.handleCallMCP))
}

// writeMCPError maps mcp package errors to HTTP responses with the
// canonical envelope.
func writeMCPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mcp.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	case errors.Is(err, mcp.ErrServerNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "mcp server not found", nil)
	case errors.Is(err, mcp.ErrToolNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "mcp tool not found", nil)
	case errors.Is(err, mcp.ErrServerNotReady):
		writeError(w, http.StatusServiceUnavailable, "MCP_SERVER_NOT_READY",
			"mcp server is not connected", nil)
	case errors.Is(err, mcp.ErrSecretLookupFailed):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	case errors.Is(err, mcp.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "MCP_NOT_CONFIGURED",
			"mcp module is not configured on this runtime", nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

// callerTenant returns the request's tenant id from tenantctx, or "" if
// none is resolved (the system / admin path).
func (s *Server) callerTenant(r *http.Request) string {
	return tenantctx.TenantID(r.Context())
}

// ─── GET /api/v1/mcp/servers ──────────────────────────────────────────────

func (s *Server) handleListMCPServers(w http.ResponseWriter, r *http.Request) {
	resp := mcpServerListResponse{Servers: []mcp.Server{}}
	if s.mcp == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	servers, err := s.mcp.Servers(r.Context(), s.callerTenant(r))
	if err != nil {
		writeMCPError(w, err)
		return
	}
	if servers != nil {
		resp.Servers = servers
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── GET /api/v1/mcp/servers/{name} ───────────────────────────────────────

func (s *Server) handleGetMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil {
		writeError(w, http.StatusServiceUnavailable, "MCP_NOT_CONFIGURED",
			"mcp module is not configured on this runtime", nil)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "name required", nil)
		return
	}
	srv, err := s.mcp.Server(r.Context(), name, s.callerTenant(r))
	if err != nil {
		writeMCPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, srv)
}

// ─── POST /api/v1/mcp/servers ─────────────────────────────────────────────

func (s *Server) handleAddMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil || s.mcpStore == nil {
		writeError(w, http.StatusServiceUnavailable, "MCP_NOT_CONFIGURED",
			"mcp module is not configured on this runtime", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in createMCPServerInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		// Default to the caller's tenant; admin callers wanting a
		// shared row can pass tenant_id="" explicitly. The handler
		// can't distinguish "didn't send the field" from "sent empty
		// string" via the JSON shape so we use the empty string as
		// the explicit "shared" signal AFTER trimming.
		tenantID = s.callerTenant(r)
	}
	upsert := mcp.UpsertInput{
		Name:        strings.TrimSpace(in.Name),
		Transport:   mcp.Transport(strings.TrimSpace(in.Transport)),
		Command:     in.Command,
		URL:         in.URL,
		Env:         in.Env,
		Description: in.Description,
		TenantID:    tenantID,
	}
	srv, err := s.mcpStore.Upsert(r.Context(), upsert)
	if err != nil {
		writeMCPError(w, err)
		return
	}
	// Kick off a reconcile so the connection comes up promptly.
	// Best-effort: a slow connect shouldn't tie up the HTTP response.
	go func(name string) {
		ctx, cancel := contextWithTimeoutBoot()
		defer cancel()
		if err := s.mcp.Reconcile(ctx); err != nil {
			s.log.Warn("mcp: post-add reconcile failed",
				"server", name, "error", err)
		}
	}(srv.Name)
	writeJSON(w, http.StatusOK, srv)
}

// ─── DELETE /api/v1/mcp/servers/{name} ────────────────────────────────────

func (s *Server) handleRemoveMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil || s.mcpStore == nil {
		writeError(w, http.StatusServiceUnavailable, "MCP_NOT_CONFIGURED",
			"mcp module is not configured on this runtime", nil)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "name required", nil)
		return
	}
	if err := s.mcpStore.Delete(r.Context(), name); err != nil {
		writeMCPError(w, err)
		return
	}
	go func(name string) {
		ctx, cancel := contextWithTimeoutBoot()
		defer cancel()
		if err := s.mcp.Reconcile(ctx); err != nil {
			s.log.Warn("mcp: post-delete reconcile failed",
				"server", name, "error", err)
		}
	}(name)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ─── PUT /api/v1/mcp/servers/{name}/enabled ───────────────────────────────

func (s *Server) handleEnableMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil || s.mcpStore == nil {
		writeError(w, http.StatusServiceUnavailable, "MCP_NOT_CONFIGURED",
			"mcp module is not configured on this runtime", nil)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "name required", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in setEnabledInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	srv, err := s.mcpStore.SetEnabled(r.Context(), name, in.Enabled)
	if err != nil {
		writeMCPError(w, err)
		return
	}
	go func(name string) {
		ctx, cancel := contextWithTimeoutBoot()
		defer cancel()
		if err := s.mcp.Reconcile(ctx); err != nil {
			s.log.Warn("mcp: post-toggle reconcile failed",
				"server", name, "error", err)
		}
	}(name)
	writeJSON(w, http.StatusOK, srv)
}

// ─── GET /api/v1/mcp/tools ────────────────────────────────────────────────

func (s *Server) handleListMCPTools(w http.ResponseWriter, r *http.Request) {
	resp := mcpToolListResponse{Tools: []mcp.Tool{}}
	server := strings.TrimSpace(r.URL.Query().Get("server"))
	tenant := s.callerTenant(r)

	// Native tool catalogue is always available when the registry is
	// wired — even if the external MCP pool isn't. We surface native
	// tools alongside external MCP tools so agents see a unified list.
	nativeToolsInjected, err := s.appendNativeMCPTools(r.Context(), &resp, server, tenant)
	if err != nil {
		writeNativeToolError(w, err)
		return
	}

	if s.mcp != nil && !nativeToolsInjected {
		tools, err := s.mcp.Tools(r.Context(), server, tenant)
		if err != nil {
			// When the filter targets a native server, the mcp pool
			// won't know about it — fall through with an empty list
			// rather than a 404.
			if !isNativeServerFilter(server) {
				writeMCPError(w, err)
				return
			}
		}
		if tools != nil {
			resp.Tools = append(resp.Tools, tools...)
		}
	} else if s.mcp != nil && nativeToolsInjected {
		// When filter targets native:* we've already populated; skip
		// the external pool.
	} else if s.mcp != nil {
		tools, err := s.mcp.Tools(r.Context(), server, tenant)
		if err != nil {
			writeMCPError(w, err)
			return
		}
		if tools != nil {
			resp.Tools = append(resp.Tools, tools...)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// appendNativeMCPTools adds native:<tool> entries to resp.Tools when the
// registry is wired. Returns (nativeOnly, err) — nativeOnly==true means
// the server filter is "native:*" so the caller should NOT additionally
// query the external pool.
func (s *Server) appendNativeMCPTools(ctx context.Context, resp *mcpToolListResponse, server, tenant string) (bool, error) {
	if s.toolsRegistry == nil {
		return isNativeServerFilter(server), nil
	}
	nativeOnly := isNativeServerFilter(server)
	if server != "" && !nativeOnly {
		return false, nil
	}
	natives, err := s.toolsRegistry.ListNativeMCPTools(ctx, tenant)
	if err != nil {
		return nativeOnly, err
	}
	for _, n := range natives {
		if nativeOnly && n.Server != server {
			continue
		}
		desc := n.Description
		resp.Tools = append(resp.Tools, mcp.Tool{
			ID:          n.ID,
			Server:      n.Server,
			Name:        n.Name,
			Description: &desc,
			InputSchema: n.InputSchema,
		})
	}
	return nativeOnly, nil
}

func isNativeServerFilter(server string) bool {
	return server != "" && strings.HasPrefix(server, "native:")
}

// ─── POST /api/v1/mcp/call ────────────────────────────────────────────────

func (s *Server) handleCallMCP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in callMCPInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	server := strings.TrimSpace(in.Server)
	tool := strings.TrimSpace(in.Tool)
	if server == "" || tool == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"server and tool are required", nil)
		return
	}

	// Route native:<tool> to the internal/tools registry. This is what
	// makes `app.mcp.call("native:browser", "navigate", {...})` work
	// from inside an agent — the agent's MCP host POSTs here, and we
	// fan out to either the external pool or the native registry.
	if tools.IsNativeServer(server) {
		if s.toolsRegistry == nil {
			writeError(w, http.StatusServiceUnavailable, "TOOLS_NOT_CONFIGURED",
				"native tool registry is not configured", nil)
			return
		}
		start := time.Now()
		result, err := s.toolsRegistry.CallNative(r.Context(), s.callerTenant(r), server, tool, in.Arguments)
		dur := time.Since(start).Milliseconds()
		if err != nil {
			writeNativeToolError(w, err)
			return
		}
		// Map the native result into the MCP CallResult shape so the
		// agent's MCP client parses it identically to external tools.
		writeJSON(w, http.StatusOK, mcp.CallResult{
			Content: []map[string]any{{
				"type": "json",
				"data": result,
			}},
			IsError:    false,
			DurationMS: dur,
		})
		return
	}

	if s.mcp == nil {
		writeError(w, http.StatusServiceUnavailable, "MCP_NOT_CONFIGURED",
			"mcp module is not configured on this runtime", nil)
		return
	}
	res, err := s.mcp.Call(r.Context(), server, tool, in.Arguments, s.callerTenant(r))
	if err != nil {
		writeMCPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// contextWithTimeoutBoot returns a background ctx with a generous
// timeout for post-mutation reconcile calls. We don't reuse the request
// ctx because the dashboard's POST returns the moment the row is
// persisted — the connect should keep going.
func contextWithTimeoutBoot() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30_000_000_000) // 30s
}

// ─── OpenAPI ──────────────────────────────────────────────────────────────

func (s *Server) registerMCPOpenAPI() {
	b := s.openapi
	b.AddTag("mcp", "Model Context Protocol servers + tools")
	b.Register("GET", "/api/v1/mcp/servers", openapi.RouteMeta{
		Summary: "List installed MCP servers", Tags: []string{"mcp"},
	})
	b.Register("GET", "/api/v1/mcp/servers/{name}", openapi.RouteMeta{
		Summary: "Get a single MCP server", Tags: []string{"mcp"},
		Parameters: []openapi.Parameter{
			{Name: "name", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("POST", "/api/v1/mcp/servers", openapi.RouteMeta{
		Summary: "Install or replace an MCP server", Tags: []string{"mcp"},
	})
	b.Register("DELETE", "/api/v1/mcp/servers/{name}", openapi.RouteMeta{
		Summary: "Remove an MCP server", Tags: []string{"mcp"},
		Parameters: []openapi.Parameter{
			{Name: "name", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("PUT", "/api/v1/mcp/servers/{name}/enabled", openapi.RouteMeta{
		Summary: "Enable or disable an MCP server", Tags: []string{"mcp"},
		Parameters: []openapi.Parameter{
			{Name: "name", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("GET", "/api/v1/mcp/tools", openapi.RouteMeta{
		Summary: "List tool catalogue (across servers or filtered)",
		Tags:    []string{"mcp"},
		Parameters: []openapi.Parameter{
			{Name: "server", In: "query", Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("POST", "/api/v1/mcp/call", openapi.RouteMeta{
		Summary: "Proxy a tools/call to the named MCP server",
		Tags:    []string{"mcp"},
	})
}
