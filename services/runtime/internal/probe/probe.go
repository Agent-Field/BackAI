// SPDX-License-Identifier: Apache-2.0

package probe

import (
	"context"
	"log/slog"
	"sync"
	"time"

	adapterregistry "github.com/Agent-Field/backai/services/runtime/internal/adapters/registry"
)

type Severity string

const (
	SeverityOK          Severity = "ok"
	SeverityDegraded    Severity = "degraded"
	SeverityUnavailable Severity = "unavailable"
)

type Result struct {
	ProbeID    string    `json:"probe_id"`
	Capability string    `json:"capability"`
	Value      any       `json:"value"`
	Severity   Severity  `json:"severity"`
	Detail     string    `json:"detail"`
	LastRun    time.Time `json:"last_run"`
	LastErr    error     `json:"-"`
}

type Probe interface {
	ID() string
	Slot() string
	Schedule() time.Duration
	Run(ctx context.Context) (Result, error)
}

type Registry struct {
	mu       sync.RWMutex
	probes   map[string]Probe
	results  map[string]Result
	log      *slog.Logger
	adapters *adapterregistry.Registry
}

func NewRegistry(log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	return &Registry{
		probes:  make(map[string]Probe),
		results: make(map[string]Result),
		log:     log,
	}
}

func (r *Registry) WithAdapterRegistry(adapters *adapterregistry.Registry) *Registry {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.adapters = adapters
	results := make([]Result, 0, len(r.results))
	for _, res := range r.results {
		results = append(results, res)
	}
	r.mu.Unlock()
	for _, res := range results {
		r.writeAdapterCapability(adapters, res)
	}
	return r
}

func (r *Registry) Register(p Probe) {
	if r == nil || p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probes[p.ID()] = p
}

func (r *Registry) RunAll(ctx context.Context) {
	if r == nil {
		return
	}
	for _, p := range r.snapshotProbes() {
		r.runProbe(ctx, p)
	}
}

func (r *Registry) Run(ctx context.Context, id string) (Result, bool) {
	if r == nil {
		return Result{}, false
	}
	r.mu.RLock()
	p, ok := r.probes[id]
	r.mu.RUnlock()
	if !ok {
		return Result{}, false
	}
	return r.runProbe(ctx, p), true
}

func (r *Registry) StartScheduled(ctx context.Context) {
	if r == nil {
		return
	}
	for _, p := range r.snapshotProbes() {
		if p.Schedule() <= 0 {
			continue
		}
		probe := p
		go func() {
			t := time.NewTicker(probe.Schedule())
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					r.runProbe(ctx, probe)
				}
			}
		}()
	}
}

func (r *Registry) Get(probeID string) (Result, bool) {
	if r == nil {
		return Result{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	res, ok := r.results[probeID]
	return res, ok
}

func (r *Registry) Snapshot() map[string]Result {
	if r == nil {
		return map[string]Result{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]Result, len(r.results))
	for k, v := range r.results {
		out[k] = v
	}
	return out
}

func (r *Registry) StoreResult(res Result) {
	if r == nil || res.ProbeID == "" {
		return
	}
	if res.LastRun.IsZero() {
		res.LastRun = time.Now().UTC()
	}
	r.mu.Lock()
	r.results[res.ProbeID] = res
	adapters := r.adapters
	r.mu.Unlock()
	r.writeAdapterCapability(adapters, res)
}

func (r *Registry) snapshotProbes() []Probe {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Probe, 0, len(r.probes))
	for _, p := range r.probes {
		out = append(out, p)
	}
	return out
}

func (r *Registry) runProbe(ctx context.Context, p Probe) Result {
	res, err := p.Run(ctx)
	if res.ProbeID == "" {
		res.ProbeID = p.ID()
	}
	if res.LastRun.IsZero() {
		res.LastRun = time.Now().UTC()
	}
	if err != nil {
		res.LastErr = err
		if res.Severity == "" {
			res.Severity = SeverityUnavailable
		}
		if res.Detail == "" {
			res.Detail = err.Error()
		}
		r.log.Warn("capability probe failed", "probe", p.ID(), "error", err)
	}
	r.StoreResult(res)
	return res
}

func (r *Registry) writeAdapterCapability(adapters *adapterregistry.Registry, res Result) {
	if adapters == nil || res.ProbeID == "" {
		return
	}
	switch res.ProbeID {
	case LiteLLMVirtualKeysProbeID:
		_ = adapters.UpdateCapability("llm-chat", "virtual_keys_active", res.Value)
		mode := "stateless"
		if active, ok := res.Value.(bool); ok && active {
			mode = "virtual_keys"
		}
		_ = adapters.UpdateCapability("llm-chat", "key_management_mode", mode)
		_ = adapters.UpdateCapability("llm-chat", "spend_tracking_exact", res.Value)
	case LiteLLMSpendTrackingProbeID:
		_ = adapters.UpdateCapability("llm-chat", "spend_tracking_active", res.Value)
	}
}
