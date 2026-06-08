// SPDX-License-Identifier: Apache-2.0

// Package exa is a stub SearchAdapter for the Exa SaaS. Every call
// returns search.ErrNotConfigured.
package exa

import (
	"context"

	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/search"
)

// Adapter is the stub.
type Adapter struct {
	apiKey string
}

// New returns a stub Exa adapter.
func New(apiKey string) *Adapter { return &Adapter{apiKey: apiKey} }

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "exa" }

// Configured always returns false (stub).
func (a *Adapter) Configured() bool { return false }

// Search is a stub.
func (a *Adapter) Search(_ context.Context, _ string, _ int) ([]search.Result, error) {
	return nil, search.ErrNotConfigured
}
