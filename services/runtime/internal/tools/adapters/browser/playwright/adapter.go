// SPDX-License-Identifier: Apache-2.0

// Package playwright is a stub BrowserAdapter for a remote Playwright
// driver. Every verb returns browser.ErrNotConfigured until the actual
// driver client is implemented.
package playwright

import (
	"context"

	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser"
)

// Adapter is the stub.
type Adapter struct {
	endpoint string
}

// New returns a stub Playwright adapter pointed at the given driver
// endpoint. Empty endpoint = unconfigured.
func New(endpoint string) *Adapter {
	return &Adapter{endpoint: endpoint}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "playwright" }

// Configured always returns false (stub).
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
