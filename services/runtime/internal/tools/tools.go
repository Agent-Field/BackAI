// SPDX-License-Identifier: Apache-2.0

// tools.go — the six Tool implementations that the factory wires into
// the Registry. Each Tool defines its verbs (with JSON-schema
// fragments) and routes Invoke calls to the active adapter.
package tools

import (
	"context"
	"fmt"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser"
	execad "github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/exec"
	fsad "github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/fs"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/httpfetch"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/search"
	sqlad "github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/sql"
)

// ─── Browser ──────────────────────────────────────────────────────────────

// BrowserTool routes to the active BrowserAdapter.
type BrowserTool struct {
	adapter browser.Adapter
}

// NewBrowserTool wraps a BrowserAdapter.
func NewBrowserTool(a browser.Adapter) *BrowserTool { return &BrowserTool{adapter: a} }

// Name returns "browser".
func (t *BrowserTool) Name() string { return string(ToolBrowser) }

// Description returns the catalogue description.
func (t *BrowserTool) Description() string {
	return "Drive a remote browser: navigate, click, fill, screenshot, extract text."
}

// AdapterID returns the active backend identifier.
func (t *BrowserTool) AdapterID() string {
	if t == nil || t.adapter == nil {
		return ""
	}
	return t.adapter.ID()
}

// Configured reflects the backing adapter's state.
func (t *BrowserTool) Configured() bool {
	if t == nil || t.adapter == nil {
		return false
	}
	return t.adapter.Configured()
}

// Verbs lists the 5 supported browser operations.
func (t *BrowserTool) Verbs() []Verb {
	return []Verb{
		{
			Name:        "navigate",
			Description: "Load a URL in a fresh or reused browser context.",
			InputSchema: objSchema(map[string]any{
				"url":        strSchema(),
				"session_id": strSchema(),
			}, []string{"url"}),
		},
		{
			Name:        "extract_text",
			Description: "Return the visible text of the current page.",
			InputSchema: objSchema(map[string]any{"session_id": strSchema()}, nil),
		},
		{
			Name:        "screenshot",
			Description: "Return a base64-encoded PNG of the current page.",
			InputSchema: objSchema(map[string]any{"session_id": strSchema()}, nil),
		},
		{
			Name:        "click",
			Description: "Click the element matching the CSS selector.",
			InputSchema: objSchema(map[string]any{
				"selector":   strSchema(),
				"session_id": strSchema(),
			}, []string{"selector"}),
		},
		{
			Name:        "fill",
			Description: "Set the value of an input matching the CSS selector.",
			InputSchema: objSchema(map[string]any{
				"selector":   strSchema(),
				"value":      strSchema(),
				"session_id": strSchema(),
			}, []string{"selector", "value"}),
		},
	}
}

// Invoke routes to the BrowserAdapter.
func (t *BrowserTool) Invoke(ctx context.Context, verb string, args map[string]any) (any, error) {
	if t.adapter == nil || !t.adapter.Configured() {
		return nil, ErrAdapterNotConfigured
	}
	sessionID := stringArg(args, "session_id")
	switch verb {
	case "navigate":
		url := stringArg(args, "url")
		if url == "" {
			return nil, fmt.Errorf("%w: url required", ErrInvalidArgs)
		}
		return t.adapter.Navigate(ctx, sessionID, url)
	case "extract_text":
		return t.adapter.ExtractText(ctx, sessionID)
	case "screenshot":
		return t.adapter.Screenshot(ctx, sessionID)
	case "click":
		sel := stringArg(args, "selector")
		if sel == "" {
			return nil, fmt.Errorf("%w: selector required", ErrInvalidArgs)
		}
		return t.adapter.Click(ctx, sessionID, sel)
	case "fill":
		sel := stringArg(args, "selector")
		val := stringArg(args, "value")
		if sel == "" {
			return nil, fmt.Errorf("%w: selector required", ErrInvalidArgs)
		}
		return t.adapter.Fill(ctx, sessionID, sel, val)
	default:
		return nil, ErrVerbNotFound
	}
}

// ─── Search ───────────────────────────────────────────────────────────────

// SearchTool routes to the active SearchAdapter.
type SearchTool struct {
	adapter search.Adapter
}

// NewSearchTool wraps a SearchAdapter.
func NewSearchTool(a search.Adapter) *SearchTool { return &SearchTool{adapter: a} }

// Name returns "search".
func (t *SearchTool) Name() string { return string(ToolSearch) }

// Description returns the catalogue description.
func (t *SearchTool) Description() string {
	return "Search the web through a configured search backend."
}

// AdapterID returns the active backend identifier.
func (t *SearchTool) AdapterID() string {
	if t == nil || t.adapter == nil {
		return ""
	}
	return t.adapter.ID()
}

// Configured reflects the backing adapter's state.
func (t *SearchTool) Configured() bool {
	if t == nil || t.adapter == nil {
		return false
	}
	return t.adapter.Configured()
}

// Verbs lists the single supported verb.
func (t *SearchTool) Verbs() []Verb {
	return []Verb{
		{
			Name:        "search",
			Description: "Run one web query and return the top results.",
			InputSchema: objSchema(map[string]any{
				"query":       strSchema(),
				"max_results": intSchema(1, 50),
			}, []string{"query"}),
		},
	}
}

// Invoke routes to the SearchAdapter.
func (t *SearchTool) Invoke(ctx context.Context, verb string, args map[string]any) (any, error) {
	if t.adapter == nil || !t.adapter.Configured() {
		return nil, ErrAdapterNotConfigured
	}
	if verb != "search" {
		return nil, ErrVerbNotFound
	}
	q := stringArg(args, "query")
	if q == "" {
		return nil, fmt.Errorf("%w: query required", ErrInvalidArgs)
	}
	maxResults := intArg(args, "max_results", 10)
	results, err := t.adapter.Search(ctx, q, maxResults)
	if err != nil {
		return nil, err
	}
	return map[string]any{"results": results}, nil
}

// ─── Filesystem ───────────────────────────────────────────────────────────

// FSTool wraps the sandbox-backed fs adapter.
type FSTool struct {
	adapter *fsad.Adapter
}

// NewFSTool constructs a filesystem Tool.
func NewFSTool(a *fsad.Adapter) *FSTool { return &FSTool{adapter: a} }

// Name returns "fs".
func (t *FSTool) Name() string { return string(ToolFS) }

// Description returns the catalogue description.
func (t *FSTool) Description() string {
	return "Operate on an ephemeral sandbox workspace (never the host filesystem)."
}

// AdapterID returns "sandbox-fs".
func (t *FSTool) AdapterID() string {
	if t == nil || t.adapter == nil {
		return "sandbox-fs"
	}
	return t.adapter.ID()
}

// Configured returns whether the sandbox runner is wired.
func (t *FSTool) Configured() bool {
	if t == nil || t.adapter == nil {
		return false
	}
	return t.adapter.Configured()
}

// Verbs lists read / write / list.
func (t *FSTool) Verbs() []Verb {
	return []Verb{
		{
			Name:        "read",
			Description: "Read a file from a caller-provided files map inside an isolated sandbox.",
			InputSchema: objSchema(map[string]any{
				"path":  strSchema(),
				"files": map[string]any{"type": "object", "additionalProperties": strSchema()},
			}, []string{"path", "files"}),
		},
		{
			Name:        "write",
			Description: "Validate a path/content pair and echo metadata (sandbox is ephemeral).",
			InputSchema: objSchema(map[string]any{
				"path":    strSchema(),
				"content": strSchema(),
			}, []string{"path", "content"}),
		},
		{
			Name:        "list",
			Description: "Return the keys of a caller-provided files map.",
			InputSchema: objSchema(map[string]any{
				"files": map[string]any{"type": "object", "additionalProperties": strSchema()},
			}, []string{"files"}),
		},
	}
}

// Invoke routes to the fs adapter.
func (t *FSTool) Invoke(ctx context.Context, verb string, args map[string]any) (any, error) {
	if t.adapter == nil || !t.adapter.Configured() {
		return nil, ErrAdapterNotConfigured
	}
	tenantID := stringArg(args, "tenant_id") // optional override; tests pass it
	switch verb {
	case "read":
		path := stringArg(args, "path")
		files := stringMapArg(args, "files")
		run, err := t.adapter.Read(ctx, tenantID, path, files)
		if err != nil {
			return nil, err
		}
		return run, nil
	case "write":
		path := stringArg(args, "path")
		content := stringArg(args, "content")
		return t.adapter.Write(ctx, tenantID, path, content)
	case "list":
		files := stringMapArg(args, "files")
		keys, err := t.adapter.List(ctx, tenantID, files)
		if err != nil {
			return nil, err
		}
		return map[string]any{"files": keys}, nil
	default:
		return nil, ErrVerbNotFound
	}
}

// ─── Exec ─────────────────────────────────────────────────────────────────

// ExecTool wraps the sandbox-backed exec adapter.
type ExecTool struct {
	adapter *execad.Adapter
}

// NewExecTool constructs an exec Tool.
func NewExecTool(a *execad.Adapter) *ExecTool { return &ExecTool{adapter: a} }

// Name returns "exec".
func (t *ExecTool) Name() string { return string(ToolExec) }

// Description returns the catalogue description.
func (t *ExecTool) Description() string {
	return "Run short-lived shell / python / node commands inside an isolated sandbox."
}

// AdapterID returns "sandbox-exec".
func (t *ExecTool) AdapterID() string {
	if t == nil || t.adapter == nil {
		return "sandbox-exec"
	}
	return t.adapter.ID()
}

// Configured returns whether the sandbox runner is wired.
func (t *ExecTool) Configured() bool {
	if t == nil || t.adapter == nil {
		return false
	}
	return t.adapter.Configured()
}

// Verbs lists shell / python / node.
func (t *ExecTool) Verbs() []Verb {
	return []Verb{
		{
			Name:        "shell",
			Description: "Run sh -lc <command> inside a sandbox image (default alpine:3.20).",
			InputSchema: objSchema(map[string]any{
				"command":   strSchema(),
				"image":     strSchema(),
				"env":       map[string]any{"type": "object", "additionalProperties": strSchema()},
				"timeout_s": intSchema(1, 300),
				"network":   map[string]any{"type": "string", "enum": []string{"isolated", "restricted", "open"}},
			}, []string{"command"}),
		},
		{
			Name:        "python",
			Description: "Run python -c <code> inside a python image (default python:3.12-slim).",
			InputSchema: objSchema(map[string]any{
				"code":      strSchema(),
				"image":     strSchema(),
				"timeout_s": intSchema(1, 300),
			}, []string{"code"}),
		},
		{
			Name:        "node",
			Description: "Run node -e <code> inside a node image (default node:20-alpine).",
			InputSchema: objSchema(map[string]any{
				"code":      strSchema(),
				"image":     strSchema(),
				"timeout_s": intSchema(1, 300),
			}, []string{"code"}),
		},
	}
}

// Invoke routes to the exec adapter.
func (t *ExecTool) Invoke(ctx context.Context, verb string, args map[string]any) (any, error) {
	if t.adapter == nil || !t.adapter.Configured() {
		return nil, ErrAdapterNotConfigured
	}
	tenantID := stringArg(args, "tenant_id")
	switch verb {
	case "shell":
		cmd := stringArg(args, "command")
		image := stringArg(args, "image")
		env := stringMapArg(args, "env")
		timeout := intArg(args, "timeout_s", 60)
		net := sandbox.NetworkMode(stringArg(args, "network"))
		return t.adapter.Shell(ctx, tenantID, cmd, image, env, timeout, net)
	case "python":
		code := stringArg(args, "code")
		image := stringArg(args, "image")
		timeout := intArg(args, "timeout_s", 60)
		return t.adapter.Python(ctx, tenantID, code, image, timeout)
	case "node":
		code := stringArg(args, "code")
		image := stringArg(args, "image")
		timeout := intArg(args, "timeout_s", 60)
		return t.adapter.Node(ctx, tenantID, code, image, timeout)
	default:
		return nil, ErrVerbNotFound
	}
}

// ─── HTTP ─────────────────────────────────────────────────────────────────

// HTTPTool wraps the safehttp adapter.
type HTTPTool struct {
	adapter *httpfetch.Adapter
}

// NewHTTPTool constructs an HTTP Tool.
func NewHTTPTool(a *httpfetch.Adapter) *HTTPTool { return &HTTPTool{adapter: a} }

// Name returns "http".
func (t *HTTPTool) Name() string { return string(ToolHTTP) }

// Description returns the catalogue description.
func (t *HTTPTool) Description() string {
	return "Make outbound HTTP calls. Private CIDRs and metadata endpoints are blocked."
}

// AdapterID returns "safehttp".
func (t *HTTPTool) AdapterID() string {
	if t == nil || t.adapter == nil {
		return "safehttp"
	}
	return t.adapter.ID()
}

// Configured is always true — safehttp ships with the runtime.
func (t *HTTPTool) Configured() bool {
	if t == nil || t.adapter == nil {
		return false
	}
	return t.adapter.Configured()
}

// Verbs lists get / post.
func (t *HTTPTool) Verbs() []Verb {
	return []Verb{
		{
			Name:        "get",
			Description: "Issue an HTTP GET. SSRF-protected.",
			InputSchema: objSchema(map[string]any{
				"url":     strSchema(),
				"headers": map[string]any{"type": "object", "additionalProperties": strSchema()},
			}, []string{"url"}),
		},
		{
			Name:        "post",
			Description: "Issue an HTTP POST with a JSON body. SSRF-protected.",
			InputSchema: objSchema(map[string]any{
				"url":     strSchema(),
				"headers": map[string]any{"type": "object", "additionalProperties": strSchema()},
				"body":    map[string]any{},
			}, []string{"url"}),
		},
	}
}

// Invoke routes to the safehttp adapter.
func (t *HTTPTool) Invoke(ctx context.Context, verb string, args map[string]any) (any, error) {
	if t.adapter == nil {
		return nil, ErrAdapterNotConfigured
	}
	url := stringArg(args, "url")
	headers := stringMapArg(args, "headers")
	switch verb {
	case "get":
		return t.adapter.Get(ctx, url, headers)
	case "post":
		return t.adapter.Post(ctx, url, headers, args["body"])
	default:
		return nil, ErrVerbNotFound
	}
}

// ─── SQL ──────────────────────────────────────────────────────────────────

// SQLTool wraps the read-only PG adapter.
type SQLTool struct {
	adapter *sqlad.Adapter
}

// NewSQLTool constructs a SQL Tool.
func NewSQLTool(a *sqlad.Adapter) *SQLTool { return &SQLTool{adapter: a} }

// Name returns "sql".
func (t *SQLTool) Name() string { return string(ToolSQL) }

// Description returns the catalogue description.
func (t *SQLTool) Description() string {
	return "Run read-only SELECT/WITH/EXPLAIN queries against the runtime's Postgres."
}

// AdapterID returns "readonly-sql".
func (t *SQLTool) AdapterID() string {
	if t == nil || t.adapter == nil {
		return "readonly-sql"
	}
	return t.adapter.ID()
}

// Configured reflects pool availability.
func (t *SQLTool) Configured() bool {
	if t == nil || t.adapter == nil {
		return false
	}
	return t.adapter.Configured()
}

// Verbs lists the single supported verb.
func (t *SQLTool) Verbs() []Verb {
	return []Verb{
		{
			Name:        "query",
			Description: "Run a parameterized read-only query (SELECT / WITH / EXPLAIN).",
			InputSchema: objSchema(map[string]any{
				"sql":      strSchema(),
				"args":     map[string]any{"type": "array"},
				"max_rows": intSchema(1, 200),
			}, []string{"sql"}),
		},
	}
}

// Invoke routes to the sql adapter.
func (t *SQLTool) Invoke(ctx context.Context, verb string, args map[string]any) (any, error) {
	if t.adapter == nil || !t.adapter.Configured() {
		return nil, ErrAdapterNotConfigured
	}
	if verb != "query" {
		return nil, ErrVerbNotFound
	}
	tenantID := stringArg(args, "tenant_id")
	sqlText := stringArg(args, "sql")
	maxRows := intArg(args, "max_rows", 50)
	var rawArgs []any
	if v, ok := args["args"].([]any); ok {
		rawArgs = v
	}
	return t.adapter.Query(ctx, tenantID, sqlText, rawArgs, maxRows)
}

// ─── helpers ──────────────────────────────────────────────────────────────

func objSchema(props map[string]any, required []string) map[string]any {
	out := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func strSchema() map[string]any { return map[string]any{"type": "string"} }
func intSchema(min, max int) map[string]any { //nolint:revive
	return map[string]any{"type": "integer", "minimum": min, "maximum": max}
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func intArg(args map[string]any, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return fallback
}

func stringMapArg(args map[string]any, key string) map[string]string {
	out := map[string]string{}
	if args == nil {
		return out
	}
	raw, ok := args[key].(map[string]any)
	if !ok {
		if typed, ok := args[key].(map[string]string); ok {
			return typed
		}
		return out
	}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}
