// SPDX-License-Identifier: Apache-2.0

// Package registry collects the runtime's wired adapter slots and
// exposes them to the dashboard via GET /api/v1/admin/adapters.
//
// At boot, main() calls Registry.Register for every slot it wires (one
// per adapter that satisfies a Go interface — sandbox, storage, etc.).
// The registry holds metadata only; it does not own the adapter
// instances themselves (the runtime's other subsystems do).
//
// Status for each slot is computed on demand by a per-slot Probe
// function the slot owner supplies. For built-in adapters the probe is
// trivial ("always healthy"); for remote adapters the probe calls
// /healthz on the sidecar. Probes are bounded by a per-call context
// timeout and the registry caches results for StatusTTL to keep the
// /admin/adapters endpoint cheap.
//
// The registry is concurrency-safe and lock-free for reads after boot
// (slots are registered once, never deleted).
package registry

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// Tier reflects how swap-able a slot is.
//
//	Tier1 — hot-swappable: env var picks one of multiple built-in adapters,
//	        or "remote" for a sidecar.
//	Tier2 — config-swappable: same wire protocol (e.g. Postgres-compatible),
//	        swap by editing a connection string.
//	Tier3 — interface-swappable: only one impl today; write your own.
//	Tier4 — foundational: not swappable (e.g. the agent runtime itself).
type Tier int

const (
	Tier1 Tier = 1
	Tier2 Tier = 2
	Tier3 Tier = 3
	Tier4 Tier = 4
)

// Kind is whether the active adapter is in-tree Go code or a remote
// sidecar reached over HTTP.
type Kind string

const (
	KindBuiltin Kind = "builtin"
	KindRemote  Kind = "remote"
	// KindNone is used for slots that are intentionally disabled (e.g.
	// AF_STACK_BILLING_ADAPTER=none).
	KindNone Kind = "none"
)

// Status is the result of a per-slot health probe.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
	StatusUnknown   Status = "unknown"
)

// Probe is a per-slot health-check function. The registry calls it
// under a bounded context; implementations MUST return promptly.
// Returning an error implies Status=unhealthy.
type Probe func(ctx context.Context) (Status, error)

// AlwaysHealthy is a convenience Probe for built-in adapters with no
// upstream to check. Returns StatusHealthy unconditionally.
func AlwaysHealthy(_ context.Context) (Status, error) {
	return StatusHealthy, nil
}

// Slot is one registered adapter slot.
//
// Capabilities is the JSON payload returned by the active adapter's
// /v1/capabilities (or a synthesized equivalent for built-in adapters
// that don't speak HTTP themselves). It's stored as RawMessage to
// avoid coupling the registry to every slot's typed shape.
type Slot struct {
	// Slot id (e.g. "sandbox", "storage"). Required.
	ID string

	// Tier signals how swappable this slot is.
	Tier Tier

	// Kind is builtin/remote/none.
	Kind Kind

	// Name of the active adapter (e.g. "docker", "stripe", "remote").
	Name string

	// Version of the active adapter, if known.
	Version string

	// Capabilities is the JSON describing what the active adapter
	// supports. Renderable directly into the /admin/adapters response.
	Capabilities json.RawMessage

	// AvailableBuiltin is the list of built-in adapter names this
	// slot can switch to without code changes (Tier1). Empty for
	// other tiers.
	AvailableBuiltin []string

	// SwapMethod tells the dashboard how to express swap. Common
	// values: "env_var", "connection_string", "none".
	SwapMethod string

	// SwapEnv is the env var the operator edits to switch adapter
	// (for env_var swap method).
	SwapEnv string

	// AdminUI is a URL to the active adapter's own admin (LiteLLM,
	// MinIO console, Stripe dashboard, etc.). Empty when none.
	AdminUI string

	// Probe is called to refresh Status. Required; use AlwaysHealthy
	// for static-OK slots.
	Probe Probe
}

// Registry collects slots. The zero value is ready to use.
type Registry struct {
	// StatusTTL bounds how often a slot's Probe is invoked. Defaults
	// to 30s when zero.
	StatusTTL time.Duration

	// ProbeTimeout caps each Probe call. Defaults to 2s when zero.
	ProbeTimeout time.Duration

	mu     sync.RWMutex
	slots  []Slot
	status map[string]statusEntry
}

type statusEntry struct {
	status    Status
	lastErr   string
	lastCheck time.Time
}

// New returns a Registry with default tunings.
func New() *Registry {
	return &Registry{
		StatusTTL:    30 * time.Second,
		ProbeTimeout: 2 * time.Second,
		status:       make(map[string]statusEntry, 16),
	}
}

// Register adds a slot. Slots are append-only — the runtime registers
// each slot exactly once at boot. Registering the same id twice
// overwrites the prior entry (useful for hot-reload-style tests).
func (r *Registry) Register(s Slot) {
	if s.ID == "" {
		panic("registry: Slot.ID required")
	}
	if s.Probe == nil {
		s.Probe = AlwaysHealthy
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.slots {
		if existing.ID == s.ID {
			r.slots[i] = s
			delete(r.status, s.ID)
			return
		}
	}
	r.slots = append(r.slots, s)
}

// SlotView is the JSON shape returned by /api/v1/admin/adapters for
// one slot. Mirrors docs/adapters/PROTOCOL.md §11.
type SlotView struct {
	Slot             string          `json:"slot"`
	Tier             int             `json:"tier"`
	Active           ActiveView      `json:"active"`
	AvailableBuiltin []string        `json:"available_builtin,omitempty"`
	SwapMethod       string          `json:"swap_method"`
	SwapEnv          string          `json:"swap_env,omitempty"`
	AdminUI          string          `json:"admin_ui,omitempty"`
}

// ActiveView is the active-adapter sub-object.
type ActiveView struct {
	Name         string          `json:"name"`
	Version      string          `json:"version,omitempty"`
	Status       Status          `json:"status"`
	Kind         Kind            `json:"kind"`
	Capabilities json.RawMessage `json:"capabilities,omitempty"`
	LastError    string          `json:"last_error,omitempty"`
}

// AdaptersResponse is the wire shape of GET /api/v1/admin/adapters.
type AdaptersResponse struct {
	Slots []SlotView `json:"slots"`
}

// List returns the view for every registered slot, refreshing status
// for any slot whose cached probe is older than StatusTTL. Sorted by
// slot id for stable rendering.
func (r *Registry) List(ctx context.Context) AdaptersResponse {
	r.mu.RLock()
	snap := make([]Slot, len(r.slots))
	copy(snap, r.slots)
	r.mu.RUnlock()

	views := make([]SlotView, 0, len(snap))
	for _, s := range snap {
		st, lastErr := r.statusFor(ctx, s)
		views = append(views, SlotView{
			Slot:             s.ID,
			Tier:             int(s.Tier),
			AvailableBuiltin: s.AvailableBuiltin,
			SwapMethod:       s.SwapMethod,
			SwapEnv:          s.SwapEnv,
			AdminUI:          s.AdminUI,
			Active: ActiveView{
				Name:         s.Name,
				Version:      s.Version,
				Status:       st,
				Kind:         s.Kind,
				Capabilities: s.Capabilities,
				LastError:    lastErr,
			},
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Slot < views[j].Slot })
	return AdaptersResponse{Slots: views}
}

// statusFor returns the cached status if still fresh, else re-probes.
// Probes are bounded by ProbeTimeout. Errors are kept as strings on
// the slot entry; the JSON view surfaces them in LastError.
func (r *Registry) statusFor(ctx context.Context, s Slot) (Status, string) {
	r.mu.RLock()
	entry, has := r.status[s.ID]
	ttl := r.StatusTTL
	r.mu.RUnlock()

	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if has && time.Since(entry.lastCheck) < ttl {
		return entry.status, entry.lastErr
	}

	timeout := r.ProbeTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	st, err := s.Probe(probeCtx)
	if err != nil {
		st = StatusUnhealthy
	}
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}

	r.mu.Lock()
	r.status[s.ID] = statusEntry{status: st, lastErr: errStr, lastCheck: time.Now()}
	r.mu.Unlock()
	return st, errStr
}

// InvalidateStatus clears the cached status for slot id, forcing a
// fresh probe on next List. Used by tests + any future operator
// "refresh now" action.
func (r *Registry) InvalidateStatus(id string) {
	r.mu.Lock()
	delete(r.status, id)
	r.mu.Unlock()
}
