// pool.go — the runtime-wide MCP connection pool.
//
// Pool owns a map of Server.Name -> Adapter. Reconcile() reads the
// authoritative state from the Store, closes adapters whose row was
// deleted or disabled, opens adapters for newly-added or re-enabled
// rows, and refreshes the cached tool catalogue.
//
// A background goroutine re-issues tools/list every refreshInterval
// (default 5 minutes). The dashboard's tool table sees stale data for
// at most that interval; explicit refresh is a future enhancement.
//
// Per-tenant filter: the public read methods (Servers, Tools) take an
// optional tenant id and only return rows where the row's tenant_id is
// NULL (shared) or matches. Pool.Call validates the same way before
// proxying.
//
// Concurrency:
//   - Reconcile / refresh hold the per-adapter open/close mutex.
//   - Servers / Tools / Call read from the in-memory map under a read
//     lock; Call dispatches against a snapshot of the adapter pointer.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// AdapterFactory builds the per-transport adapter for one row. Injected
// into the Pool so the mcp package itself doesn't import its own
// adapter sub-packages (which would create an import cycle: adapters
// already depend on mcp for the Adapter / Tool / CallResult types).
//
// resolvedEnv is the row's env map after ResolveEnv has replaced every
// "secret:<key>" value with the vault plaintext. The factory is free
// to use this as a process env (stdio) or header map (sse).
//
// main.go wires the default factory in NewDefaultFactory; tests can
// stub with their own.
type AdapterFactory func(srv Server, resolvedEnv map[string]string, log *slog.Logger) (Adapter, error)

// Pool is the in-memory connection set. Construct one per runtime
// process via NewPool.
type Pool struct {
	store           *Store
	vault           SecretReader
	log             *slog.Logger
	refreshInterval time.Duration
	factory         AdapterFactory

	mu       sync.RWMutex
	adapters map[string]*entry // by server name
	tools    map[string][]Tool // by server name; refreshed on connect + tick
	servers  map[string]Server // cached row snapshot by name
}

type entry struct {
	adapter Adapter
	server  Server
}

// PoolConfig collects optional knobs. Defaults below are safe for
// production.
type PoolConfig struct {
	// RefreshInterval controls how often the Pool re-issues tools/list
	// against every ready adapter. Defaults to 5 minutes.
	RefreshInterval time.Duration
	// Logger is the slog handle. Defaults to slog.Default().
	Logger *slog.Logger
	// Factory builds adapters from rows. Required — main.go wires
	// DefaultFactory at boot; the mcp package itself can't reference
	// its own adapter sub-packages without an import cycle.
	Factory AdapterFactory
}

// NewPool constructs a Pool. store must not be nil (use a Store backed
// by a nil pool if the runtime is in "no DB" mode — every method then
// degrades gracefully). vault may be nil; "secret:<key>" env values
// will fail to resolve in that case.
func NewPool(store *Store, vault SecretReader, cfg PoolConfig) *Pool {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	refresh := cfg.RefreshInterval
	if refresh <= 0 {
		refresh = time.Duration(DefaultRefreshInterval) * time.Second
	}
	return &Pool{
		store:           store,
		vault:           vault,
		log:             log,
		refreshInterval: refresh,
		factory:         cfg.Factory,
		adapters:        map[string]*entry{},
		tools:           map[string][]Tool{},
		servers:         map[string]Server{},
	}
}

// Configured reports whether the Pool is backed by a real Store. The
// HTTP layer uses this to translate "no pool" into 503.
func (p *Pool) Configured() bool {
	return p != nil && p.store != nil && p.store.HasPool()
}

// Servers returns the cached row snapshots filtered by tenant. An empty
// tenantID returns every row (the dashboard's admin view). A non-empty
// tenantID returns rows where row.tenant_id is NULL or matches.
func (p *Pool) Servers(_ context.Context, tenantID string) ([]Server, error) {
	if p == nil {
		return nil, ErrNotConfigured
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Server, 0, len(p.servers))
	for _, srv := range p.servers {
		if !tenantMatches(srv.TenantID, tenantID) {
			continue
		}
		out = append(out, srv)
	}
	return sortServers(out), nil
}

// Server returns one cached row by name, or ErrServerNotFound. Applies
// the same tenant filter as Servers.
func (p *Pool) Server(_ context.Context, name, tenantID string) (Server, error) {
	if p == nil {
		return Server{}, ErrNotConfigured
	}
	p.mu.RLock()
	srv, ok := p.servers[name]
	p.mu.RUnlock()
	if !ok {
		return Server{}, ErrServerNotFound
	}
	if !tenantMatches(srv.TenantID, tenantID) {
		return Server{}, ErrServerNotFound
	}
	return srv, nil
}

// Tools returns the cached tool catalogue. When server == "" the result
// is the union over every ready adapter the caller's tenant is allowed
// to see. When server != "" only that server's tools are returned.
func (p *Pool) Tools(_ context.Context, server, tenantID string) ([]Tool, error) {
	if p == nil {
		return nil, ErrNotConfigured
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := []Tool{}
	if server != "" {
		srv, ok := p.servers[server]
		if !ok || !tenantMatches(srv.TenantID, tenantID) {
			return nil, ErrServerNotFound
		}
		tools := p.tools[server]
		out = append(out, tools...)
		return out, nil
	}
	for name, srv := range p.servers {
		if !tenantMatches(srv.TenantID, tenantID) {
			continue
		}
		out = append(out, p.tools[name]...)
	}
	return out, nil
}

// Call proxies a tool invocation to the named server. The call inherits
// ctx's deadline; when ctx has none, DefaultCallTimeoutSec applies.
func (p *Pool) Call(ctx context.Context, server, tool string, args map[string]any, tenantID string) (CallResult, error) {
	if p == nil {
		return CallResult{}, ErrNotConfigured
	}
	p.mu.RLock()
	srv, srvOk := p.servers[server]
	e, eOk := p.adapters[server]
	p.mu.RUnlock()
	if !srvOk || !tenantMatches(srv.TenantID, tenantID) {
		return CallResult{}, ErrServerNotFound
	}
	if !eOk || e.adapter == nil {
		return CallResult{}, ErrServerNotReady
	}
	if srv.Status != StatusReady {
		return CallResult{}, ErrServerNotReady
	}
	if tool == "" {
		return CallResult{}, fmt.Errorf("%w: tool required", ErrInvalidInput)
	}
	// Apply default timeout when caller didn't set one.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(DefaultCallTimeoutSec)*time.Second)
		defer cancel()
	}
	start := time.Now()
	res, err := e.adapter.Call(ctx, tool, args)
	res.DurationMS = time.Since(start).Milliseconds()
	return res, err
}

// Reconcile aligns the in-memory adapter set with the persisted Store.
//
// For every row visible to the bypass-RLS context:
//   - is_enabled=false: close any open adapter; status -> disabled.
//   - is_enabled=true + no adapter: open one; on success status ->
//     ready, on failure status -> errored with the error message.
//   - is_enabled=true + open adapter: leave it (refresh handles tools).
//
// For every adapter NOT in the row set (deleted): close it.
//
// Safe for concurrent calls; later callers wait on the same mutex.
func (p *Pool) Reconcile(ctx context.Context) error {
	if !p.Configured() {
		return ErrNotConfigured
	}
	rows, err := p.store.List(ctx)
	if err != nil {
		return fmt.Errorf("mcp: reconcile list: %w", err)
	}
	desired := map[string]Server{}
	for _, srv := range rows {
		desired[srv.Name] = srv
	}

	p.mu.Lock()
	// 1) Tear down adapters whose row vanished or was disabled.
	for name, e := range p.adapters {
		srv, stillThere := desired[name]
		if !stillThere {
			p.log.Info("mcp: closing removed adapter", "server", name)
			_ = e.adapter.Close()
			delete(p.adapters, name)
			delete(p.tools, name)
			continue
		}
		if !srv.IsEnabled {
			p.log.Info("mcp: closing disabled adapter", "server", name)
			_ = e.adapter.Close()
			delete(p.adapters, name)
			delete(p.tools, name)
		}
	}
	// Snapshot the rows we'd need to open next.
	type openTask struct {
		name string
		srv  Server
	}
	tasks := []openTask{}
	for name, srv := range desired {
		if !srv.IsEnabled {
			continue
		}
		if _, alreadyOpen := p.adapters[name]; alreadyOpen {
			continue
		}
		tasks = append(tasks, openTask{name: name, srv: srv})
	}
	// Refresh the cached row snapshots regardless.
	p.servers = map[string]Server{}
	for name, srv := range desired {
		p.servers[name] = srv
	}
	p.mu.Unlock()

	// 2) Open new adapters. Done outside the lock so a slow connect
	//    (network IO) doesn't block read methods.
	for _, t := range tasks {
		p.openOne(ctx, t.srv)
	}
	return nil
}

// openOne builds the adapter for one row, performs the handshake,
// fetches the initial tool catalogue, stamps status, and stores the
// connection. Errors are recorded via Store.RecordStatus so the
// dashboard sees them.
func (p *Pool) openOne(ctx context.Context, srv Server) {
	tenantID := ""
	if srv.TenantID != nil {
		tenantID = *srv.TenantID
	}
	resolvedEnv, err := ResolveEnv(ctx, srv.Env, p.vault, tenantID)
	if err != nil {
		p.markErrored(ctx, srv.Name, err)
		return
	}
	if p.factory == nil {
		p.markErrored(ctx, srv.Name, fmt.Errorf("%w: no adapter factory wired", ErrInvalidInput))
		return
	}
	adapter, err := p.factory(srv, resolvedEnv, p.log)
	if err != nil {
		p.markErrored(ctx, srv.Name, err)
		return
	}
	if err := adapter.Connect(ctx); err != nil {
		_ = adapter.Close()
		p.markErrored(ctx, srv.Name, err)
		return
	}
	tools, err := adapter.ListTools(ctx)
	if err != nil {
		_ = adapter.Close()
		p.markErrored(ctx, srv.Name, err)
		return
	}

	p.mu.Lock()
	p.adapters[srv.Name] = &entry{adapter: adapter, server: srv}
	p.tools[srv.Name] = tools
	if cur, ok := p.servers[srv.Name]; ok {
		cur.Status = StatusReady
		cur.LastError = nil
		cur.ToolsCount = len(tools)
		p.servers[srv.Name] = cur
	}
	p.mu.Unlock()

	if err := p.store.RecordStatus(ctx, srv.Name, StatusReady, ""); err != nil {
		p.log.Warn("mcp: record ready failed", "server", srv.Name, "error", err)
	}
	if err := p.store.RecordToolsCount(ctx, srv.Name, len(tools)); err != nil {
		p.log.Warn("mcp: record tools_count failed", "server", srv.Name, "error", err)
	}
	p.log.Info("mcp: adapter ready",
		"server", srv.Name,
		"transport", srv.Transport,
		"tools", len(tools),
	)
}

func (p *Pool) markErrored(ctx context.Context, name string, err error) {
	msg := err.Error()
	if len(msg) > MaxLastErrorChars {
		msg = msg[:MaxLastErrorChars]
	}
	p.log.Warn("mcp: adapter errored", "server", name, "error", msg)
	p.mu.Lock()
	if cur, ok := p.servers[name]; ok {
		cur.Status = StatusErrored
		ptr := msg
		cur.LastError = &ptr
		p.servers[name] = cur
	}
	p.mu.Unlock()
	if storeErr := p.store.RecordStatus(ctx, name, StatusErrored, msg); storeErr != nil {
		p.log.Warn("mcp: record errored failed", "server", name, "error", storeErr)
	}
}

// RefreshTools re-issues tools/list against every ready adapter and
// updates the cache + tools_count column. Errors from one adapter don't
// short-circuit the others.
func (p *Pool) RefreshTools(ctx context.Context) {
	p.mu.RLock()
	snapshot := make([]*entry, 0, len(p.adapters))
	for _, e := range p.adapters {
		snapshot = append(snapshot, e)
	}
	p.mu.RUnlock()
	for _, e := range snapshot {
		tools, err := e.adapter.ListTools(ctx)
		if err != nil {
			p.log.Warn("mcp: refresh tools failed", "server", e.server.Name, "error", err)
			continue
		}
		p.mu.Lock()
		p.tools[e.server.Name] = tools
		if cur, ok := p.servers[e.server.Name]; ok {
			cur.ToolsCount = len(tools)
			p.servers[e.server.Name] = cur
		}
		p.mu.Unlock()
		if err := p.store.RecordToolsCount(ctx, e.server.Name, len(tools)); err != nil {
			p.log.Warn("mcp: record tools_count failed", "server", e.server.Name, "error", err)
		}
	}
}

// RunRefreshLoop blocks until ctx is cancelled, calling RefreshTools
// every refreshInterval. Spawn on a goroutine from main.go.
func (p *Pool) RunRefreshLoop(ctx context.Context) {
	t := time.NewTicker(p.refreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.RefreshTools(ctx)
		}
	}
}

// Shutdown closes every adapter. Called from main on graceful exit.
func (p *Pool) Shutdown() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, e := range p.adapters {
		_ = e.adapter.Close()
		delete(p.adapters, name)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────

// tenantMatches enforces the per-tenant visibility rule: a tenant sees
// rows where row.tenant_id is NULL (shared) or matches. An empty
// caller tenant is the "admin/system" path and sees every row.
func tenantMatches(rowTenant *string, callerTenant string) bool {
	if callerTenant == "" {
		return true
	}
	if rowTenant == nil {
		return true
	}
	return *rowTenant == callerTenant
}

// sortServers returns the slice sorted by Name. Inline to avoid pulling
// in sort.Slice — callers always render a small list.
func sortServers(in []Server) []Server {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j-1].Name > in[j].Name; j-- {
			in[j-1], in[j] = in[j], in[j-1]
		}
	}
	return in
}

// keep errors import alive across refactors
var _ = errors.Is
