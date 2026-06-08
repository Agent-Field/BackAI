// SPDX-License-Identifier: Apache-2.0

// Package steel is a stub BrowserAdapter for the Steel.dev SaaS. It
// implements the browser.Adapter interface but every verb returns
// browser.ErrNotConfigured until a real Steel client is wired and
// STEEL_API_KEY is set in the environment.
//
// This stub exists so the factory can return a working tool object
// (so the dashboard renders the card with adapter = "steel") even when
// the operator hasn't wired Steel — they see a clear "Configure" prompt
// instead of a silent fallback to browser-use.
package steel

import (
	"context"

	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser"
)

// Adapter is the stub. Configured() returns true only when apiKey is
// non-empty; even in that case the verbs still return ErrNotConfigured
// because the real client isn't implemented yet.
type Adapter struct {
	apiKey string
}

// New returns a stub Steel adapter with the given API key. An empty key
// produces an unconfigured adapter — Configured() returns false.
func New(apiKey string) *Adapter {
	return &Adapter{apiKey: apiKey}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "steel" }

// Configured reports whether STEEL_API_KEY is set.
//
// NOTE: even when this returns true, the verbs still return
// ErrNotConfigured — the Steel client itself is not yet implemented.
// We surface "configured" so the dashboard can show the API key has
// been seen, but every invocation still falls through to the stub.
func (a *Adapter) Configured() bool { return false }

// Navigate is a stub.
func (a *Adapter) Navigate(_ context.Context, _, _ string) (browser.Result, error) {
	return browser.Result{}, browser.ErrNotConfigured
}

// ExtractText is a stub.
func (a *Adapter) ExtractText(_ context.Context, _ string) (browser.Result, error) {
	return browser.Result{}, browser.ErrNotConfigured
}

// Screenshot is a stub.
func (a *Adapter) Screenshot(_ context.Context, _ string) (browser.Result, error) {
	return browser.Result{}, browser.ErrNotConfigured
}

// Click is a stub.
func (a *Adapter) Click(_ context.Context, _, _ string) (browser.Result, error) {
	return browser.Result{}, browser.ErrNotConfigured
}

// Fill is a stub.
func (a *Adapter) Fill(_ context.Context, _, _, _ string) (browser.Result, error) {
	return browser.Result{}, browser.ErrNotConfigured
}
