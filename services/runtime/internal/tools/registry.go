// SPDX-License-Identifier: Apache-2.0

// registry.go — the runtime-wide table of registered Tool instances.
//
// The Registry holds one Tool per canonical name (ToolBrowser /
// ToolSearch / ...). It also owns the per-tenant enable layer, which
// reads/writes the `suite_tenant_tools` row for the active tenant.
//
// The HTTP layer (server/tools_native.go) and the MCP bridge
// (mcp_bridge.go) both consume this registry.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
	"github.com/Agent-Field/backai/services/runtime/internal/toolstats"
)

// Registry is the central tool table + per-tenant enable layer.
// Construct via NewRegistry from the boot code.
type Registry struct {
	mu    sync.RWMutex
	tools map[ToolName]Tool
	pool  *pgxpool.Pool
	log   *slog.Logger
	stats *toolstats.Recorder
}

func (r *Registry) SetStatsRecorder(rec *toolstats.Recorder) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats = rec
}

// NewRegistry returns an empty registry. Pass nil pool to disable the
// per-tenant enable layer (the dashboard then shows all tools as
// disabled by default; SetEnabled returns ErrNotConfigured).
func NewRegistry(pool *pgxpool.Pool, log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	return &Registry{
		tools: map[ToolName]Tool{},
		pool:  pool,
		log:   log,
	}
}

// Register adds a Tool to the registry, replacing any existing entry
// with the same Name. Safe to call from boot only — the registry isn't
// designed for hot-swap.
func (r *Registry) Register(t Tool) {
	if r == nil || t == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[ToolName(t.Name())] = t
}

// Tool returns the Tool for the given name, or ErrToolNotFound.
func (r *Registry) Tool(name ToolName) (Tool, error) {
	if r == nil {
		return nil, ErrNotConfigured
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, ErrToolNotFound
	}
	return t, nil
}

// All returns every registered Tool, sorted by name. Used by the MCP
// bridge to advertise the catalogue.
func (r *Registry) All() []Tool {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Configured reports whether the registry has any tools registered.
func (r *Registry) Configured() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools) > 0
}

// ListStatus returns the per-tool status (adapter + verbs + enable
// flag + config) for the given tenant. Tools never registered are
// omitted; tools registered but without a row default to disabled.
func (r *Registry) ListStatus(ctx context.Context, tenantID string) ([]TenantToolStatus, error) {
	if r == nil {
		return nil, ErrNotConfigured
	}
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	all := r.All()
	enables, configs, updates := map[ToolName]bool{}, map[ToolName]map[string]any{}, map[ToolName]string{}
	if r.pool != nil && tenantID != "" {
		rows, err := r.pool.Query(ctx, `
			select tool, enabled, config, updated_at
			from suite_tenant_tools
			where tenant_id = $1
		`, tenantID)
		if err != nil {
			return nil, fmt.Errorf("tools: list status: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				tool    string
				enabled bool
				raw     []byte
				updated time.Time
			)
			if err := rows.Scan(&tool, &enabled, &raw, &updated); err != nil {
				return nil, err
			}
			n := ToolName(tool)
			enables[n] = enabled
			configs[n] = decodeJSONMap(raw)
			updates[n] = updated.UTC().Format(time.RFC3339Nano)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	out := make([]TenantToolStatus, 0, len(all))
	for _, t := range all {
		name := ToolName(t.Name())
		cfg := configs[name]
		if cfg == nil {
			cfg = map[string]any{}
		}
		status := TenantToolStatus{
			Tool:        name,
			Description: t.Description(),
			AdapterID:   t.AdapterID(),
			Configured:  t.Configured(),
			Enabled:     enables[name],
			Verbs:       t.Verbs(),
			Config:      cfg,
		}
		if ts, ok := updates[name]; ok {
			s := ts
			status.UpdatedAt = &s
		}
		out = append(out, status)
	}
	return out, nil
}

// SetEnabled upserts the per-tenant enable row. Returns the persisted
// status so the caller can echo it to the client.
func (r *Registry) SetEnabled(ctx context.Context, tenantID string, name ToolName, enabled bool, config map[string]any) (TenantToolStatus, error) {
	if r == nil {
		return TenantToolStatus{}, ErrNotConfigured
	}
	if r.pool == nil {
		return TenantToolStatus{}, ErrNotConfigured
	}
	if tenantID == "" {
		return TenantToolStatus{}, ErrTenantRequired
	}
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	if !IsValidToolName(string(name)) {
		return TenantToolStatus{}, ErrToolNotFound
	}
	tool, err := r.Tool(name)
	if err != nil {
		return TenantToolStatus{}, err
	}
	if config == nil {
		config = map[string]any{}
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return TenantToolStatus{}, fmt.Errorf("%w: config must be JSON-serializable", ErrInvalidArgs)
	}
	var (
		outEnabled bool
		outRaw     []byte
		updated    time.Time
	)
	err = r.pool.QueryRow(ctx, `
		insert into suite_tenant_tools (tenant_id, tool, enabled, config, created_at, updated_at)
		values ($1, $2, $3, $4, now(), now())
		on conflict (tenant_id, tool) do update set
			enabled = excluded.enabled,
			config = excluded.config,
			updated_at = now()
		returning enabled, config, updated_at
	`, tenantID, string(name), enabled, raw).Scan(&outEnabled, &outRaw, &updated)
	if err != nil {
		return TenantToolStatus{}, fmt.Errorf("tools: set enabled: %w", err)
	}
	ts := updated.UTC().Format(time.RFC3339Nano)
	return TenantToolStatus{
		Tool:        name,
		Description: tool.Description(),
		AdapterID:   tool.AdapterID(),
		Configured:  tool.Configured(),
		Enabled:     outEnabled,
		Verbs:       tool.Verbs(),
		Config:      decodeJSONMap(outRaw),
		UpdatedAt:   &ts,
	}, nil
}

// IsEnabled reports whether the given tool is enabled for the tenant.
// When pool is nil OR tenant is empty, returns false (closed by
// default for safety in unauthenticated paths).
func (r *Registry) IsEnabled(ctx context.Context, tenantID string, name ToolName) (bool, error) {
	if r == nil || r.pool == nil || tenantID == "" {
		return false, nil
	}
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	var enabled bool
	err := r.pool.QueryRow(ctx, `
		select enabled from suite_tenant_tools
		where tenant_id = $1 and tool = $2
	`, tenantID, string(name)).Scan(&enabled)
	if err != nil {
		// Row missing == disabled (default state)
		return false, nil
	}
	return enabled, nil
}

// Invoke is the canonical entry point for executing a verb. It
// enforces the per-tenant enable, fetches the Tool, and routes to its
// Invoke method.
//
// When tenantID is empty (boot-mode / system call) the enable check is
// skipped — the caller is trusted.
func (r *Registry) Invoke(ctx context.Context, tenantID string, name ToolName, verb string, args map[string]any) (out any, err error) {
	started := time.Now()
	defer func() {
		var rec *toolstats.Recorder
		if r != nil {
			r.mu.RLock()
			rec = r.stats
			r.mu.RUnlock()
		}
		if rec != nil {
			rec.RecordResult(ctx, tenantID, string(name), toolstats.TransportNative, started, err)
		}
	}()
	if r == nil {
		return nil, ErrNotConfigured
	}
	tool, err := r.Tool(name)
	if err != nil {
		return nil, err
	}
	if tenantID != "" {
		enabled, _ := r.IsEnabled(ctx, tenantID, name)
		if !enabled {
			return nil, ErrAdapterDisabled
		}
	}
	if !tool.Configured() {
		return nil, ErrAdapterNotConfigured
	}
	return tool.Invoke(ctx, verb, args)
}

// decodeJSONMap unmarshals a JSONB column into map[string]any, returning
// an empty map (never nil) on error or null.
func decodeJSONMap(raw []byte) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out
}

// CheckConfigured returns nil unless the active adapter for a tool
// type returned an "adapter not configured" stub but the operator
// explicitly named that adapter via env. Used at boot to surface a
// clear error (rather than fall back silently).
//
// Example: AF_STACK_TOOL_BROWSER=steel but STEEL_API_KEY is empty —
// the factory still constructs the steel stub; this check produces a
// human-readable error the boot loop can print and exit on.
func CheckConfigured(toolName ToolName, adapterID string, configured bool) error {
	if configured {
		return nil
	}
	return fmt.Errorf(
		"tools: %s adapter %q is selected via env but not configured; "+
			"set the required env vars or pick a different adapter",
		toolName, adapterID,
	)
}

// Ensure errors are visible in the test binary.
var _ = errors.New
