// SPDX-License-Identifier: Apache-2.0

// Package tavily is a stub SearchAdapter for the Tavily SaaS. Every
// call returns search.ErrNotConfigured until the real client lands and
// TAVILY_API_KEY is set.
package tavily

import (
	"context"

	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/search"
)

// Adapter is the stub.
type Adapter struct {
	apiKey string
}

// New returns a stub Tavily adapter.
func New(apiKey string) *Adapter { return &Adapter{apiKey: apiKey} }

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "tavily" }

// Configured always returns false (stub).
func (a *Adapter) Configured() bool { return false }

// Search is a stub.
func (a *Adapter) Search(_ context.Context, _ string, _ int) ([]search.Result, error) {
	return nil, search.ErrNotConfigured
}
