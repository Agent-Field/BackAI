// SPDX-License-Identifier: Apache-2.0

// Package duckduckgo is a stub SearchAdapter for the DuckDuckGo
// instant-answer API. Every call returns search.ErrNotConfigured.
// Listed for completeness; lower-quality fallback when no other
// adapter is wired.
package duckduckgo

import (
	"context"

	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/search"
)

// Adapter is the stub.
type Adapter struct{}

// New returns a stub DuckDuckGo adapter.
func New() *Adapter { return &Adapter{} }

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "duckduckgo" }

// Configured always returns false (stub).
func (a *Adapter) Configured() bool { return false }

// Search is a stub.
func (a *Adapter) Search(_ context.Context, _ string, _ int) ([]search.Result, error) {
	return nil, search.ErrNotConfigured
}
