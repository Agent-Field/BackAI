// SPDX-License-Identifier: Apache-2.0

// Package tools is AF Stack's first-party agent tool surface.
//
// This package provides a uniform Tool/ToolAdapter contract for the six
// built-in tool types every agent runtime needs: browser, web search,
// filesystem, code execution, HTTP fetch, and read-only SQL. The
// operator picks WHICH backend implements each type via env vars (e.g.
// AF_STACK_TOOL_BROWSER=browser-use|steel|playwright); the agent
// surface is identical regardless of the chosen adapter.
//
// # Three layers
//
//  1. The `Tool` contract — verbs (with input/output schemas) plus an
//     Invoke(verb, args) entry point. Each of the six tool TYPES has
//     exactly one Tool registered in the runtime at boot.
//  2. The per-type `ToolAdapter` interfaces (BrowserAdapter,
//     SearchAdapter, etc.) — each tool type uses one of these to back
//     its verbs. The factory in factory.go reads the env and picks one
//     adapter per type; unconfigured choices return the
//     ErrAdapterNotConfigured stub.
//  3. The MCP bridge (mcp_bridge.go) — exposes the registered tools
//     to agents under the `native:<tool>` namespace so any agent can
//     call them via `app.mcp.call("native:browser", "navigate", {...})`.
//
// # Per-tenant enable
//
// The runtime persists a `suite_tenant_tools` row per (tenant, tool).
// Disabled tools are 403 for the tenant; the dashboard renders a toggle
// per card. AgentField owns the actual tool-call spans/traces — this
// package only routes the request and enforces the per-tenant enable.
//
// # Relationship to internal/tooladapters
//
// `internal/tooladapters` is the existing monolithic service that
// shipped with Phase 2; it remains the source of truth for the current
// `/api/v1/tools/adapters` and `/api/v1/tools/call` routes. This new
// package adds a stricter Tool/ToolAdapter interface, env-driven
// adapter selection, and the MCP `native:` namespace; it lives
// side-by-side so existing callers keep working.
package tools

import (
	"context"
	"errors"
)

// ─── Sentinel errors ──────────────────────────────────────────────────────

var (
	// ErrToolNotFound is returned when the registry has no Tool for the
	// requested name.
	ErrToolNotFound = errors.New("tools: tool not found")

	// ErrVerbNotFound is returned when a Tool has no verb with the
	// requested name.
	ErrVerbNotFound = errors.New("tools: verb not found")

	// ErrAdapterNotConfigured is returned by stub adapters whose env
	// vars are not set (e.g. Steel without STEEL_API_KEY). The server
	// translates this into a 503 TOOL_ADAPTER_NOT_CONFIGURED.
	ErrAdapterNotConfigured = errors.New("tools: adapter not configured")

	// ErrAdapterDisabled is returned by the registry when the requested
	// tool is disabled for the current tenant.
	ErrAdapterDisabled = errors.New("tools: adapter disabled for tenant")

	// ErrInvalidArgs is returned when the caller's args don't match a
	// verb's input schema (or are otherwise malformed).
	ErrInvalidArgs = errors.New("tools: invalid arguments")

	// ErrTenantRequired is returned when an action that needs a tenant
	// context is invoked without one (e.g. agent call with no resolver).
	ErrTenantRequired = errors.New("tools: tenant required")

	// ErrNotConfigured is returned by the service when the registry was
	// never wired (boot mode without DB / sandbox / safehttp etc.).
	ErrNotConfigured = errors.New("tools: registry not configured")
)

// ─── Tool contract ────────────────────────────────────────────────────────

// Verb is one callable action on a Tool. Name is the verb identifier
// (e.g. "navigate" on the browser tool). InputSchema and OutputSchema
// are JSON Schema fragments — the MCP bridge surfaces InputSchema in
// the catalogue so agents can prompt-engineer correct arguments.
type Verb struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"input_schema"`
	OutputSchema map[string]any `json:"output_schema,omitempty"`
}

// Tool is the per-tool-type contract. Every registered tool has a
// stable Name (e.g. "browser"), a Description (used in the MCP
// catalogue), a fixed Verbs list, and an Invoke(verb, args) entry
// point. Implementations are constructed by the factory at boot and
// are safe for concurrent use.
type Tool interface {
	// Name returns the tool identifier ("browser" / "search" / "fs" /
	// "exec" / "http" / "sql"). MUST match one of the values in the
	// registered set.
	Name() string

	// Description is the human-readable string surfaced in catalogues.
	Description() string

	// AdapterID is the active backend identifier (e.g. "browser-use",
	// "searxng"). Surfaced so operators can see which backend is wired
	// without inspecting env.
	AdapterID() string

	// Configured reports whether the backing adapter has everything it
	// needs (API keys, endpoint URL, sandbox available). When false the
	// Invoke method returns ErrAdapterNotConfigured.
	Configured() bool

	// Verbs returns the supported verbs in declaration order. The list
	// is static for the lifetime of the Tool.
	Verbs() []Verb

	// Invoke executes one verb. Implementations:
	//   - validate that `verb` is a known name (else ErrVerbNotFound)
	//   - validate args against the verb's input schema (or at least the
	//     required keys) — else ErrInvalidArgs
	//   - return a JSON-serializable value on success
	//   - return ErrAdapterNotConfigured if the adapter is a stub
	Invoke(ctx context.Context, verb string, args map[string]any) (any, error)
}

// ─── Tool-type identifiers ────────────────────────────────────────────────

// ToolName values are the canonical identifiers used in:
//   - the registry's per-type key
//   - the dashboard card slug
//   - the MCP namespace (`native:<name>`)
//   - the `suite_tenant_tools.tool` column
const (
	ToolBrowser ToolName = "browser"
	ToolSearch  ToolName = "search"
	ToolFS      ToolName = "fs"
	ToolExec    ToolName = "exec"
	ToolHTTP    ToolName = "http"
	ToolSQL     ToolName = "sql"
)

// ToolName is the typed string for a tool identifier.
type ToolName string

// AllToolNames is the canonical list, used by the dashboard / the MCP
// bridge / the per-tenant enable scan. Kept in stable order so the
// dashboard renders cards consistently.
var AllToolNames = []ToolName{
	ToolBrowser,
	ToolSearch,
	ToolFS,
	ToolExec,
	ToolHTTP,
	ToolSQL,
}

// IsValidToolName reports whether n is one of the canonical names.
// Used by the HTTP layer to validate path params.
func IsValidToolName(n string) bool {
	for _, v := range AllToolNames {
		if string(v) == n {
			return true
		}
	}
	return false
}

// ─── Per-tenant enable status ─────────────────────────────────────────────

// TenantToolStatus is the row shape persisted in `suite_tenant_tools`
// and surfaced via the REST list endpoint. It combines the tool's
// adapter metadata with the per-tenant enable flag so the dashboard
// can render each card in a single fetch.
type TenantToolStatus struct {
	Tool        ToolName       `json:"tool"`
	Description string         `json:"description"`
	AdapterID   string         `json:"adapter_id"`
	Configured  bool           `json:"configured"`
	Enabled     bool           `json:"enabled"`
	Verbs       []Verb         `json:"verbs"`
	Config      map[string]any `json:"config"`
	UpdatedAt   *string        `json:"updated_at"`
}
