// Package hooks implements named hook points for cross-cutting concerns.
//
// Handlers register against a HookPoint (gateway.pre_auth, llm.pre_call,
// sandbox.pre_run, etc.). When Fire is called, registered handlers run in
// registration order, each receiving (and possibly modifying) the payload.
//
// Any handler returning a non-nil error aborts the chain and the error is
// returned to the caller. Use this to short-circuit (e.g. PII guard rejects
// an LLM call).
//
// Payloads are typed as `any` for flexibility; modules document the expected
// shape per hook point. A typed adapter (HookFunc[T]) is provided for safety.
package hooks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// HookPoint is a named extension slot.
type HookPoint string

// The canonical set of hook points exposed by the suite runtime and modules.
// New hook points may be added as modules land.
const (
	HookGatewayPreAuth   HookPoint = "gateway.pre_auth"
	HookGatewayPostAuth  HookPoint = "gateway.post_auth"
	HookAFPreExecute     HookPoint = "af.pre_execute"
	HookAFPostExecute    HookPoint = "af.post_execute"
	HookLLMPreCall       HookPoint = "llm.pre_call"
	HookLLMPostCall      HookPoint = "llm.post_call"
	HookSandboxPreRun    HookPoint = "sandbox.pre_run"
	HookSandboxPostRun   HookPoint = "sandbox.post_run"
	HookStoragePreUpload HookPoint = "storage.pre_upload"
	HookNotifPreSend     HookPoint = "notifications.pre_send"
	HookBillingPreCharge HookPoint = "billing.pre_charge"
	HookTenantCreated    HookPoint = "tenant.created"
)

// Handler is a single function bound to a hook point.
//
// It receives the context and an in/out payload pointer-or-value. It may
// modify the payload and must return it (potentially the same instance) and
// an error. Returning a non-nil error short-circuits the chain.
type Handler func(ctx context.Context, payload any) (any, error)

// Engine is the hook dispatcher.
type Engine struct {
	mu       sync.RWMutex
	log      *slog.Logger
	handlers map[HookPoint][]Handler
}

// NewEngine constructs an empty hook engine.
func NewEngine(log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{
		log:      log,
		handlers: make(map[HookPoint][]Handler),
	}
}

// Register adds a handler to the given hook point. Handlers fire in the
// order they were registered.
func (e *Engine) Register(point HookPoint, h Handler) error {
	if h == nil {
		return errors.New("hooks: nil handler")
	}
	if point == "" {
		return errors.New("hooks: empty point")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[point] = append(e.handlers[point], h)
	return nil
}

// Fire invokes all handlers registered for point in order, passing the
// payload through each. If a handler returns an error, the chain stops and
// the error is returned. If no handlers are registered, the payload is
// returned unchanged.
func (e *Engine) Fire(ctx context.Context, point HookPoint, payload any) (any, error) {
	e.mu.RLock()
	handlers := append([]Handler(nil), e.handlers[point]...)
	e.mu.RUnlock()

	for i, h := range handlers {
		next, err := h(ctx, payload)
		if err != nil {
			e.log.Debug("hook chain aborted",
				"point", point,
				"handler_index", i,
				"error", err,
			)
			return payload, fmt.Errorf("hook %s[%d]: %w", point, i, err)
		}
		payload = next
	}
	return payload, nil
}

// Count returns the number of handlers registered for point.
func (e *Engine) Count(point HookPoint) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.handlers[point])
}

// Points returns all hook points that currently have at least one handler.
func (e *Engine) Points() []HookPoint {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]HookPoint, 0, len(e.handlers))
	for p := range e.handlers {
		out = append(out, p)
	}
	return out
}
