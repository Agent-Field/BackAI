// SPDX-License-Identifier: Apache-2.0

package tooladapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/safehttp"
	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

type SandboxRunner interface {
	Run(ctx context.Context, spec sandbox.RunSpec) (*sandbox.SandboxRun, error)
}

type Config struct {
	SearXNGURL        string
	BrowserUseURL     string
	DatabaseAvailable bool
	SandboxAvailable  bool
}

type Service struct {
	pool       *pgxpool.Pool
	sandbox    SandboxRunner
	httpClient *http.Client
	cfg        Config
	log        *slog.Logger
}

func New(pool *pgxpool.Pool, sandboxRunner SandboxRunner, cfg Config, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	cfg.DatabaseAvailable = pool != nil
	cfg.SandboxAvailable = sandboxRunner != nil
	return &Service{
		pool:       pool,
		sandbox:    sandboxRunner,
		httpClient: safehttp.New(safehttp.Options{Timeout: 30 * time.Second}),
		cfg:        cfg,
		log:        log,
	}
}

func (s *Service) List(ctx context.Context) (List, error) {
	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return List{Adapters: []Adapter{}}, ErrTenantRequired
	}
	defs := Catalog(s.cfg)
	byID := map[AdapterID]Adapter{}
	for _, def := range defs {
		byID[def.ID] = Adapter{
			ID:             def.ID,
			Label:          def.Label,
			Description:    def.Description,
			Enabled:        def.DefaultEnabled,
			Configured:     def.Configured,
			DefaultEnabled: def.DefaultEnabled,
			Config:         map[string]any{},
			Tools:          cloneTools(def.Tools),
		}
	}
	if s.pool != nil {
		rows, err := s.pool.Query(ctx, `
			select adapter, enabled, config, updated_at
			from suite_tool_adapters
			where tenant_id = $1
		`, tenantID)
		if err != nil {
			return List{Adapters: []Adapter{}}, fmt.Errorf("tooladapters: list: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				id      AdapterID
				enabled bool
				raw     []byte
				updated time.Time
			)
			if err := rows.Scan(&id, &enabled, &raw, &updated); err != nil {
				return List{Adapters: []Adapter{}}, err
			}
			adapter, ok := byID[id]
			if !ok {
				continue
			}
			adapter.Enabled = enabled
			adapter.Config = decodeMap(raw)
			ts := updated.UTC().Format(time.RFC3339Nano)
			adapter.UpdatedAt = &ts
			byID[id] = adapter
		}
		if err := rows.Err(); err != nil {
			return List{Adapters: []Adapter{}}, err
		}
	}
	out := make([]Adapter, 0, len(defs))
	for _, def := range defs {
		out = append(out, byID[def.ID])
	}
	return List{Adapters: out}, nil
}

func (s *Service) SetEnabled(ctx context.Context, id AdapterID, in SetEnabledInput) (Adapter, error) {
	if s.pool == nil {
		return Adapter{}, ErrNotConfigured
	}
	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return Adapter{}, ErrTenantRequired
	}
	// Bind tenant so the RLS force policy (app.tenant_id GUC) permits the
	// upsert on suite_tool_adapters. "" apiKeyID is safe.
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	def, ok := definitionByID(id, s.cfg)
	if !ok {
		return Adapter{}, ErrNotFound
	}
	config := in.Config
	if config == nil {
		config = map[string]any{}
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return Adapter{}, fmt.Errorf("%w: config must be JSON-serializable", ErrInvalidInput)
	}
	userID := tenantctx.UserID(ctx)
	var (
		enabled bool
		outRaw  []byte
		updated time.Time
	)
	err = s.pool.QueryRow(ctx, `
		insert into suite_tool_adapters
			(tenant_id, adapter, enabled, config, updated_by, created_at, updated_at)
		values ($1, $2, $3, $4, nullif($5, '')::uuid, now(), now())
		on conflict (tenant_id, adapter) do update set
			enabled = excluded.enabled,
			config = excluded.config,
			updated_by = excluded.updated_by,
			updated_at = now()
		returning enabled, config, updated_at
	`, tenantID, string(id), in.Enabled, raw, userID).Scan(&enabled, &outRaw, &updated)
	if err != nil {
		return Adapter{}, fmt.Errorf("tooladapters: set enabled: %w", err)
	}
	ts := updated.UTC().Format(time.RFC3339Nano)
	return Adapter{
		ID: id, Label: def.Label, Description: def.Description,
		Enabled: enabled, Configured: def.Configured, DefaultEnabled: def.DefaultEnabled,
		Config: decodeMap(outRaw), UpdatedAt: &ts, Tools: cloneTools(def.Tools),
	}, nil
}

func (s *Service) Call(ctx context.Context, in CallInput) (CallResult, error) {
	start := time.Now()
	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return CallResult{}, ErrTenantRequired
	}
	if !adapterPattern.MatchString(string(in.Adapter)) {
		return CallResult{}, ErrInvalidInput
	}
	if strings.TrimSpace(in.Tool) == "" {
		return CallResult{}, fmt.Errorf("%w: tool required", ErrInvalidInput)
	}
	adapter, err := s.getAdapter(ctx, in.Adapter)
	if err != nil {
		return CallResult{}, err
	}
	if !adapter.Enabled {
		return CallResult{}, ErrDisabled
	}
	if !adapter.Configured {
		return CallResult{}, ErrUnavailable
	}

	var result any
	switch in.Adapter {
	case AdapterHTTP:
		result, err = s.callHTTP(ctx, in.Tool, in.Arguments)
	case AdapterSQL:
		result, err = s.callSQL(ctx, in.Tool, in.Arguments, tenantID)
	case AdapterExec:
		result, err = s.callExec(ctx, in.Tool, in.Arguments, tenantID)
	case AdapterFS:
		result, err = s.callFS(ctx, in.Tool, in.Arguments, tenantID)
	case AdapterSearXNG:
		result, err = s.callSearXNG(ctx, in.Tool, in.Arguments)
	case AdapterBrowserUse:
		result, err = s.callBrowserUse(ctx, in.Tool, in.Arguments)
	default:
		err = ErrNotFound
	}

	duration := time.Since(start).Milliseconds()
	status := "succeeded"
	var errText string
	if err != nil {
		status = "failed"
		errText = err.Error()
	}
	s.recordCall(ctx, CallRecord{
		TenantID: tenantID, Adapter: in.Adapter, Tool: in.Tool,
		Status: status, DurationMS: duration, Error: errText,
	})
	if err != nil {
		return CallResult{}, err
	}
	return CallResult{
		Adapter: in.Adapter, Tool: in.Tool, Status: status,
		Result: result, DurationMS: duration,
	}, nil
}

func (s *Service) getAdapter(ctx context.Context, id AdapterID) (Adapter, error) {
	list, err := s.List(ctx)
	if err != nil {
		return Adapter{}, err
	}
	for _, adapter := range list.Adapters {
		if adapter.ID == id {
			return adapter, nil
		}
	}
	return Adapter{}, ErrNotFound
}

func (s *Service) callHTTP(ctx context.Context, tool string, args map[string]any) (any, error) {
	if tool != "request" {
		return nil, ErrNotFound
	}
	u, err := stringArg(args, "url", true)
	if err != nil {
		return nil, err
	}
	method, _ := stringArg(args, "method", false)
	if method == "" {
		method = http.MethodGet
	}
	method = strings.ToUpper(method)
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead:
	default:
		return nil, fmt.Errorf("%w: unsupported method %s", ErrInvalidInput, method)
	}
	var body io.Reader
	if v, ok := args["body"]; ok {
		raw, _ := json.Marshal(v)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid url", ErrInvalidInput)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range stringMapArg(args, "headers") {
		req.Header.Set(k, v)
	}
	client := s.httpClient
	if timeout := intArg(args, "timeout_s", 0); timeout > 0 {
		client = safehttp.New(safehttp.Options{Timeout: time.Duration(timeout) * time.Second})
	}
	resp, err := client.Do(req)
	if err != nil {
		// The SSRF guard's rejection message embeds the resolved internal
		// IP + CIDR; collapse it to a sanitized sentinel so the handler
		// returns a 400 without leaking host-network topology.
		if safehttp.IsBlocked(err) {
			return nil, ErrBlockedDestination
		}
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return map[string]any{
		"status_code": resp.StatusCode,
		"headers":     flattenHeaders(resp.Header),
		"body":        string(raw),
	}, nil
}

func (s *Service) callSQL(ctx context.Context, tool string, args map[string]any, tenantID string) (any, error) {
	if tool != "query" {
		return nil, ErrNotFound
	}
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	sqlText, err := stringArg(args, "sql", true)
	if err != nil {
		return nil, err
	}
	sqlText, err = validateReadOnlySQL(sqlText)
	if err != nil {
		return nil, err
	}
	maxRows := intArg(args, "max_rows", 50)
	if maxRows <= 0 || maxRows > 100 {
		maxRows = 50
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("tooladapters: sql begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		return nil, fmt.Errorf("tooladapters: sql tenant context: %w", err)
	}
	rows, err := tx.Query(ctx, sqlText)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = f.Name
	}
	outRows := []map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, v := range vals {
			row[columns[i]] = jsonValue(v)
		}
		outRows = append(outRows, row)
		if len(outRows) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"columns": columns, "rows": outRows}, nil
}

func (s *Service) callExec(ctx context.Context, tool string, args map[string]any, tenantID string) (any, error) {
	if tool != "run" {
		return nil, ErrNotFound
	}
	if s.sandbox == nil {
		return nil, ErrUnavailable
	}
	image, _ := stringArg(args, "image", false)
	if image == "" {
		image = "alpine:3.20"
	}
	command := stringSliceArg(args, "command")
	if len(command) == 0 {
		return nil, fmt.Errorf("%w: command required", ErrInvalidInput)
	}
	spec := sandbox.RunSpec{
		TenantID: tenantID, Image: image, Command: command,
		Files: stringMapArg(args, "files"), Env: stringMapArg(args, "env"),
		TimeoutS:    intArg(args, "timeout_s", 30),
		Network:     sandbox.NetworkMode(strings.ToLower(stringValue(args["network"]))),
		AllowEgress: stringSliceArg(args, "allow_egress"),
	}
	if spec.Network == "" {
		spec.Network = sandbox.NetworkIsolated
	}
	run, err := s.sandbox.Run(ctx, spec)
	if err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Service) callFS(ctx context.Context, tool string, args map[string]any, tenantID string) (any, error) {
	path, err := stringArg(args, "path", true)
	if err != nil {
		return nil, err
	}
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("%w: path must be relative and stay inside workspace", ErrInvalidInput)
	}
	switch tool {
	case "read":
		files := stringMapArg(args, "files")
		if _, ok := files[path]; !ok {
			return nil, fmt.Errorf("%w: path missing from files map", ErrInvalidInput)
		}
		run, err := s.sandbox.Run(ctx, sandbox.RunSpec{
			TenantID: tenantID, Image: "alpine:3.20",
			Command: []string{"sh", "-lc", "cat -- " + shellQuote(path)},
			Files:   files, TimeoutS: 10, Network: sandbox.NetworkIsolated,
		})
		if err != nil {
			return nil, err
		}
		return run, nil
	case "write":
		content, err := stringArg(args, "content", true)
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": path, "bytes": len([]byte(content))}, nil
	default:
		return nil, ErrNotFound
	}
}

func (s *Service) callSearXNG(ctx context.Context, tool string, args map[string]any) (any, error) {
	if tool != "search" || s.cfg.SearXNGURL == "" {
		return nil, ErrUnavailable
	}
	q, err := stringArg(args, "q", true)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(strings.TrimRight(s.cfg.SearXNGURL, "/") + "/search")
	if err != nil {
		return nil, fmt.Errorf("%w: invalid searxng url", ErrUnavailable)
	}
	qs := base.Query()
	qs.Set("q", q)
	qs.Set("format", "json")
	for _, key := range []string{"categories", "language", "time_range"} {
		if v, _ := stringArg(args, key, false); v != "" {
			qs.Set(key, v)
		}
	}
	base.RawQuery = qs.Encode()
	return s.callHTTP(ctx, "request", map[string]any{"url": base.String(), "method": "GET"})
}

func (s *Service) callBrowserUse(ctx context.Context, tool string, args map[string]any) (any, error) {
	if tool != "run" || s.cfg.BrowserUseURL == "" {
		return nil, ErrUnavailable
	}
	task, err := stringArg(args, "task", true)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(s.cfg.BrowserUseURL, "/") + "/run"
	return s.callHTTP(ctx, "request", map[string]any{
		"url": endpoint, "method": "POST",
		"body": map[string]any{"task": task, "timeout_s": intArg(args, "timeout_s", 120)},
	})
}

func (s *Service) recordCall(ctx context.Context, rec CallRecord) {
	if s.pool == nil || rec.TenantID == "" {
		return
	}
	// Bind tenant so the RLS force policy permits the audit insert on
	// suite_tool_adapter_calls. "" apiKeyID is safe.
	ctx = tenantctx.WithTenant(ctx, rec.TenantID, "")
	if len(rec.Error) > 500 {
		rec.Error = rec.Error[:500]
	}
	_, err := s.pool.Exec(ctx, `
		insert into suite_tool_adapter_calls
			(tenant_id, adapter, tool, status, duration_ms, error, created_at)
		values ($1, $2, $3, $4, $5, nullif($6, ''), now())
	`, rec.TenantID, string(rec.Adapter), rec.Tool, rec.Status, int(rec.DurationMS), rec.Error)
	if err != nil && s.log != nil {
		s.log.Warn("tooladapters: call audit write failed", "error", err)
	}
}

func definitionByID(id AdapterID, cfg Config) (AdapterDefinition, bool) {
	for _, def := range Catalog(cfg) {
		if def.ID == id {
			return def, true
		}
	}
	return AdapterDefinition{}, false
}

func decodeMap(raw []byte) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out
}

func cloneTools(in []ToolDefinition) []ToolDefinition {
	out := make([]ToolDefinition, len(in))
	copy(out, in)
	return out
}

var blockedSQL = regexp.MustCompile(`(?i)\b(insert|update|delete|drop|alter|create|truncate|grant|revoke|copy|call|do|vacuum|analyze|refresh|listen|notify|set|reset)\b`)

func validateReadOnlySQL(sqlText string) (string, error) {
	trimmed := strings.TrimSpace(sqlText)
	trimmed = strings.TrimSuffix(trimmed, ";")
	lower := strings.ToLower(strings.TrimSpace(trimmed))
	if lower == "" {
		return "", fmt.Errorf("%w: sql required", ErrInvalidInput)
	}
	if strings.Contains(lower, ";") {
		return "", fmt.Errorf("%w: multiple statements are not allowed", ErrInvalidInput)
	}
	if !(strings.HasPrefix(lower, "select ") || strings.HasPrefix(lower, "with ") || strings.HasPrefix(lower, "explain ")) {
		return "", fmt.Errorf("%w: only select/with/explain queries are allowed", ErrInvalidInput)
	}
	if blockedSQL.MatchString(lower) {
		return "", fmt.Errorf("%w: mutating SQL is not allowed", ErrInvalidInput)
	}
	return trimmed, nil
}

func flattenHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, vals := range h {
		out[k] = strings.Join(vals, ",")
	}
	return out
}

func jsonValue(v any) any {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	default:
		return x
	}
}

func stringArg(args map[string]any, key string, required bool) (string, error) {
	v := strings.TrimSpace(stringValue(args[key]))
	if v == "" && required {
		return "", fmt.Errorf("%w: %s required", ErrInvalidInput, key)
	}
	return v, nil
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func intArg(args map[string]any, key string, fallback int) int {
	switch v := args[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return fallback
	}
}

func stringMapArg(args map[string]any, key string) map[string]string {
	out := map[string]string{}
	raw, ok := args[key].(map[string]any)
	if !ok {
		if typed, ok := args[key].(map[string]string); ok {
			return typed
		}
		return out
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s, ok := raw[k].(string); ok {
			out[k] = s
		}
	}
	return out
}

func stringSliceArg(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		if typed, ok := args[key].([]string); ok {
			return typed
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

var _ = pgx.ErrNoRows
