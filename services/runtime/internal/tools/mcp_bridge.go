// SPDX-License-Identifier: Apache-2.0

// mcp_bridge.go — exposes the registered Tools as MCP tools under the
// `native:` namespace so agents call them via:
//
//	app.mcp.call("native:browser", "navigate", {"url": "..."})
//
// The bridge is a thin shim over the Registry. The agent runtime
// (AgentField) treats `native:<tool>` as just another MCP server —
// nothing changes on the agent side.
//
// The bridge:
//   - lists native tools (one MCP "tool" per Verb of every registered Tool)
//   - routes a call to native:<tool>/<verb> back to Registry.Invoke
//
// Per-tenant filtering: ListNativeMCPTools returns ONLY the verbs of
// tools that are (a) configured and (b) enabled for the caller's tenant
// — disabled tools shouldn't appear in the catalogue so the agent can't
// even try to call them.
package tools

import (
	"context"
	"fmt"
	"strings"
)

// MCPNativePrefix is the namespace under which native tools appear in
// the MCP catalogue. The agent calls them as
// `app.mcp.call("native:browser", "navigate", {...})`.
const MCPNativePrefix = "native:"

// MCPNativeTool mirrors the MCP catalogue row shape (mcp.Tool) but is
// declared here so the tools package doesn't import internal/mcp
// (which would create a circular dep when the mcp package eventually
// surfaces native tools in its own list).
type MCPNativeTool struct {
	ID          string         `json:"id"`     // "native:browser/navigate"
	Server      string         `json:"server"` // "native:browser"
	Name        string         `json:"name"`   // "navigate"
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// ListNativeMCPTools returns the per-tenant view of native tools as
// MCP catalogue rows. Disabled or unconfigured tools are omitted.
//
// When tenantID is empty (system/boot path) every configured tool is
// returned regardless of the enable state.
func (r *Registry) ListNativeMCPTools(ctx context.Context, tenantID string) ([]MCPNativeTool, error) {
	if r == nil {
		return nil, ErrNotConfigured
	}
	out := []MCPNativeTool{}
	for _, t := range r.All() {
		if !t.Configured() {
			continue
		}
		if tenantID != "" {
			enabled, _ := r.IsEnabled(ctx, tenantID, ToolName(t.Name()))
			if !enabled {
				continue
			}
		}
		server := MCPNativePrefix + t.Name()
		for _, v := range t.Verbs() {
			out = append(out, MCPNativeTool{
				ID:          server + "/" + v.Name,
				Server:      server,
				Name:        v.Name,
				Description: v.Description,
				InputSchema: v.InputSchema,
			})
		}
	}
	return out, nil
}

// IsNativeServer reports whether `server` names a native tool (i.e.
// starts with the `native:` prefix). The MCP host calls this to decide
// whether to route a call into the Registry or into its external
// connection pool.
func IsNativeServer(server string) bool {
	return strings.HasPrefix(server, MCPNativePrefix)
}

// CallNative routes an MCP call against a native server to the
// Registry. The server name is expected to be "native:<tool>" — the
// returned value is whatever Tool.Invoke produced (the MCP bridge in
// the HTTP layer wraps it in the MCP content shape).
func (r *Registry) CallNative(ctx context.Context, tenantID, server, verb string, args map[string]any) (any, error) {
	if r == nil {
		return nil, ErrNotConfigured
	}
	if !IsNativeServer(server) {
		return nil, fmt.Errorf("%w: server %q is not a native tool", ErrToolNotFound, server)
	}
	name := ToolName(strings.TrimPrefix(server, MCPNativePrefix))
	if !IsValidToolName(string(name)) {
		return nil, ErrToolNotFound
	}
	return r.Invoke(ctx, tenantID, name, verb, args)
}

// AvailableNativeTools returns the names of every configured + enabled
// native tool for the given tenant. The agent's `__capabilities__`
// reasoner uses this to declare what's available at probe time.
func (r *Registry) AvailableNativeTools(ctx context.Context, tenantID string) []string {
	if r == nil {
		return nil
	}
	out := []string{}
	for _, t := range r.All() {
		if !t.Configured() {
			continue
		}
		if tenantID != "" {
			enabled, _ := r.IsEnabled(ctx, tenantID, ToolName(t.Name()))
			if !enabled {
				continue
			}
		}
		out = append(out, MCPNativePrefix+t.Name())
	}
	return out
}
