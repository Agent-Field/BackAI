// Package modules implements the module + adapter system that lets the
// runtime load suite primitives at startup.
//
// A module is declared by a `manifest.yaml` file under `modules/<id>/`.
// It can:
//   - declare dependencies on other modules (init order)
//   - declare config schema (validated at boot)
//   - register HTTP routes
//   - register hook handlers
//
// Modules are wired in code rather than reflected from filesystem during
// v1, so Init takes a slice of Module instances. The filesystem manifest
// is the source of truth for *metadata* (display name, version, deps)
// and exists alongside the Go code that implements the module.
package modules

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// Module is the contract every suite primitive implements.
//
// Lifecycle: Manifest() → Init(ctx, deps) → Routes()/Hooks() → Shutdown(ctx).
// The Registry calls these in dependency order.
type Module interface {
	// Manifest returns metadata for this module. Read at registration.
	Manifest() Manifest

	// Init does the heavy lifting: open connections, run migrations,
	// register hooks. Init is called in dependency order.
	Init(ctx context.Context, deps Deps) error

	// Shutdown releases module resources. Called in reverse-init order.
	Shutdown(ctx context.Context) error
}

// Manifest is module metadata.
type Manifest struct {
	ID           string   // unique; matches the modules/ subfolder name
	Name         string   // human-readable
	Version      string   // semver
	Description  string   // one-line
	Dependencies []string // other module IDs that must init first
}

// Deps groups runtime-level dependencies injected into modules.
type Deps struct {
	Logger *slog.Logger
	// More dependencies (DB, AF client, hook engine, etc.) will land as
	// modules need them. We keep this list small and explicit.
}

// Registry holds registered modules and orchestrates lifecycle.
type Registry struct {
	log     *slog.Logger
	modules map[string]Module
	order   []string // resolved init order (dependency-sorted)
	started []string // modules that have completed Init; used for shutdown order
}

// NewRegistry constructs an empty Registry.
func NewRegistry(log *slog.Logger) *Registry {
	return &Registry{
		log:     log,
		modules: make(map[string]Module),
	}
}

// Register adds a module to the registry. Order doesn't matter — Resolve
// computes dependency order.
//
// Returns an error if a module with the same ID is already registered.
func (r *Registry) Register(m Module) error {
	id := m.Manifest().ID
	if id == "" {
		return errors.New("modules: empty manifest ID")
	}
	if _, exists := r.modules[id]; exists {
		return fmt.Errorf("modules: %q already registered", id)
	}
	r.modules[id] = m
	return nil
}

// Resolve computes the init order based on declared dependencies. Detects
// missing deps and cycles. Must be called before Start.
func (r *Registry) Resolve() error {
	// Standard topological sort with cycle detection.
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	order := make([]string, 0, len(r.modules))

	var visit func(id string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("modules: dependency cycle detected involving %q", id)
		}
		visiting[id] = true
		mod, ok := r.modules[id]
		if !ok {
			return fmt.Errorf("modules: missing module %q (referenced as dependency)", id)
		}
		for _, dep := range mod.Manifest().Dependencies {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		order = append(order, id)
		return nil
	}

	for id := range r.modules {
		if err := visit(id); err != nil {
			return err
		}
	}
	r.order = order
	return nil
}

// Start initializes all registered modules in dependency order.
//
// If any module fails Init, the registry shuts down already-started
// modules (in reverse order) and returns the error.
func (r *Registry) Start(ctx context.Context, deps Deps) error {
	if r.order == nil {
		if err := r.Resolve(); err != nil {
			return err
		}
	}
	for _, id := range r.order {
		mod := r.modules[id]
		r.log.Info("module init", "id", id, "version", mod.Manifest().Version)
		if err := mod.Init(ctx, deps); err != nil {
			r.log.Error("module init failed; rolling back", "id", id, "error", err)
			r.shutdownStarted(ctx)
			return fmt.Errorf("modules: init %q: %w", id, err)
		}
		r.started = append(r.started, id)
	}
	return nil
}

// Stop shuts down all started modules in reverse-init order.
//
// Errors from individual modules are logged but do not stop the loop —
// every module gets a chance to shut down.
func (r *Registry) Stop(ctx context.Context) {
	r.shutdownStarted(ctx)
}

func (r *Registry) shutdownStarted(ctx context.Context) {
	for i := len(r.started) - 1; i >= 0; i-- {
		id := r.started[i]
		mod := r.modules[id]
		r.log.Info("module shutdown", "id", id)
		if err := mod.Shutdown(ctx); err != nil {
			r.log.Error("module shutdown failed", "id", id, "error", err)
		}
	}
	r.started = nil
}

// IDs returns the resolved init order (or registration order if Resolve
// hasn't run). For tests and diagnostics.
func (r *Registry) IDs() []string {
	if r.order != nil {
		out := make([]string, len(r.order))
		copy(out, r.order)
		return out
	}
	out := make([]string, 0, len(r.modules))
	for id := range r.modules {
		out = append(out, id)
	}
	return out
}
