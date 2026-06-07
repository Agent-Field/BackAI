// Package mcp is AF Stack's Model Context Protocol runtime: a long-lived
// pool of connections to external MCP servers (stdio child processes or
// HTTP+SSE endpoints) plus a uniform Adapter contract every transport
// implements.
//
// The Pool reconciles the connection set against the persisted
// suite_mcp_servers table, refreshes the tool catalogue on a 5-minute
// tick, and proxies tool calls with per-call timeouts. The HTTP layer
// (services/runtime/internal/server/mcp.go) is a thin REST veneer over
// the Pool's surface — every wire shape mirrors the Phase-11.1 zod
// schemas in apps/dashboard/src/lib/api.ts.
//
// Contract notes
//
//	Field names, enum values, and nullable semantics here MUST mirror
//	MCPServerSchema / MCPToolSchema / CallMCPResultSchema in
//	apps/dashboard/src/lib/api.ts exactly. Drift breaks the dashboard's
//	safeParse and (via Phase 11.2) the Python+TS SDK's typed wrappers.
package mcp

import (
	"context"
	"errors"
)

// Transport enumerates the wire formats the runtime can speak to MCP
// servers over. Mirrors MCPTransportSchema.
type Transport string

const (
	// TransportStdio spawns a child process via os/exec and talks
	// JSON-RPC over stdin/stdout, framed with `Content-Length:
	// N\r\n\r\n<json>` per the MCP spec.
	TransportStdio Transport = "stdio"

	// TransportSSE opens a long-lived HTTP+SSE connection. The runtime
	// POSTs JSON-RPC requests to <url>/messages and receives responses
	// on a Server-Sent-Events stream from <url>/sse.
	TransportSSE Transport = "sse"
)

// Status is the lifecycle phase persisted in suite_mcp_servers.status
// and surfaced on MCPServerSchema.status.
type Status string

const (
	// StatusConnecting means Pool.Reconcile is trying to bring this
	// connection up. The initial row state when first added; the
	// transient state during reconnect.
	StatusConnecting Status = "connecting"

	// StatusReady means the handshake + initial tools/list both
	// succeeded. The only state in which Pool.Call will dispatch.
	StatusReady Status = "ready"

	// StatusErrored means the last connect or call returned an error.
	// last_error carries the truncated message.
	StatusErrored Status = "errored"

	// StatusDisabled means the operator flipped is_enabled=false. The
	// adapter is closed but the row is preserved for re-enable.
	StatusDisabled Status = "disabled"
)

// Server mirrors MCPServerSchema in apps/dashboard/src/lib/api.ts and
// the suite_mcp_servers row. Nullable wire fields use pointer types so
// json.Marshal emits null rather than the zero value.
type Server struct {
	Name            string            `json:"name"`
	Transport       Transport         `json:"transport"`
	Command         []string          `json:"command"`
	URL             *string           `json:"url"`
	Env             map[string]string `json:"env"`
	TenantID        *string           `json:"tenant_id"`
	Description     string            `json:"description"`
	IsEnabled       bool              `json:"is_enabled"`
	Status          Status            `json:"status"`
	LastError       *string           `json:"last_error"`
	ToolsCount      int               `json:"tools_count"`
	InstalledAt     string            `json:"installed_at"`
	LastConnectedAt *string           `json:"last_connected_at"`
}

// Tool mirrors MCPToolSchema. The composite ID is "<server>/<name>" so
// the agent layer (Phase 11.2) can pass a single string through tool-use
// prompting and the runtime can route it without an extra lookup table.
type Tool struct {
	ID          string         `json:"id"`
	Server      string         `json:"server"`
	Name        string         `json:"name"`
	Description *string        `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// CallResult mirrors CallMCPResultSchema. We pass MCP's native `content`
// array through unchanged so the dashboard + SDK can pull text / image /
// json parts as they please.
type CallResult struct {
	Content    []map[string]any `json:"content"`
	IsError    bool             `json:"is_error"`
	DurationMS int64            `json:"duration_ms"`
}

// UpsertInput collects the fields a Store.Upsert call needs. Mirrors
// CreateMCPServerInputSchema; the Store applies defaults (empty command,
// empty env, empty description) for absent values.
type UpsertInput struct {
	Name        string
	Transport   Transport
	Command     []string
	URL         string
	Env         map[string]string
	Description string
	TenantID    string
}

// Adapter is the per-server transport contract. The Pool owns one
// Adapter instance per active Server row.
//
// Lifecycle:
//
//	Connect    — perform the MCP handshake (initialize/initialized).
//	             Idempotent on a fresh Adapter; called once per
//	             open-from-closed transition.
//	ListTools  — issue tools/list. Cached by the Pool; called on
//	             Connect and on the 5-minute refresh tick.
//	Call       — issue tools/call. Returns the MCP content array +
//	             is_error flag. The Pool wraps this with a per-call
//	             timeout from ctx.
//	Close      — terminate the underlying transport. For stdio this is
//	             a SIGTERM followed by a short grace window then SIGKILL;
//	             for SSE this is a cancellation of the read goroutine.
//
// Adapters are NOT required to be safe for concurrent Connect/Close from
// different goroutines — the Pool serialises lifecycle calls per
// adapter. List/Call ARE called concurrently and implementations must
// be safe for that.
type Adapter interface {
	Connect(ctx context.Context) error
	ListTools(ctx context.Context) ([]Tool, error)
	Call(ctx context.Context, tool string, args map[string]any) (CallResult, error)
	Close() error
}

// ─── Sentinel errors ──────────────────────────────────────────────────────

var (
	// ErrNotConfigured is returned when the runtime has no MCP pool
	// wired (e.g. no DB at boot). The HTTP layer maps it to 503
	// MCP_NOT_CONFIGURED.
	ErrNotConfigured = errors.New("mcp: not configured")

	// ErrServerNotFound — no row matched the requested name (and, when
	// MT is on, tenant_id).
	ErrServerNotFound = errors.New("mcp: server not found")

	// ErrServerNotReady — the Pool has the row but the adapter isn't
	// connected (status != ready). Distinguishes "your config is wrong"
	// from "we know you, just give us a moment".
	ErrServerNotReady = errors.New("mcp: server not ready")

	// ErrToolNotFound — the cached tool catalogue for the named server
	// has no entry for the requested tool name.
	ErrToolNotFound = errors.New("mcp: tool not found")

	// ErrInvalidInput — the upsert / call input failed validation.
	ErrInvalidInput = errors.New("mcp: invalid input")

	// ErrSecretLookupFailed — a value prefixed "secret:<key>" could
	// not be resolved against the vault. Wrapped error has the
	// underlying vault error.
	ErrSecretLookupFailed = errors.New("mcp: secret lookup failed")
)

// SecretReader is the minimal contract the env-resolver needs from the
// secrets vault. Kept as an interface so unit tests don't pull in a
// real pgxpool. Matches secrets.Vault.Get.
type SecretReader interface {
	Get(ctx context.Context, tenantID, key string) ([]byte, error)
}

// MaxLastErrorChars caps the persisted last_error column so the
// dashboard isn't drowned in multi-MB stack traces.
const MaxLastErrorChars = 500

// DefaultCallTimeoutSec is the per-tool-call timeout when the caller
// hasn't set a deadline on the ctx. Generous enough for a model-backed
// MCP tool to finish, tight enough that a stuck stdio child doesn't
// pin an HTTP request goroutine.
const DefaultCallTimeoutSec = 30

// DefaultRefreshInterval is how often Pool re-issues tools/list against
// every ready adapter. Picked to match the spec's "every 5 minutes"
// refresh cadence.
const DefaultRefreshInterval = 5 * 60 // seconds; converted to time.Duration in pool.go
