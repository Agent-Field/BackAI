// SPDX-License-Identifier: Apache-2.0

// Package search defines the SearchAdapter contract — the per-backend
// interface that the runtime's `search` tool routes verb invocations
// to. Five adapters live under sub-packages:
//
//   - searxng    — primary (self-hostable; default)
//   - tavily     — paid SaaS (stub until TAVILY_API_KEY is set)
//   - brave      — paid SaaS (stub until BRAVE_API_KEY is set)
//   - exa        — paid SaaS (stub until EXA_API_KEY is set)
//   - duckduckgo — zero-key (stub for now; lower-quality fallback)
package search

import (
	"context"
	"errors"
)

// ErrNotConfigured is the shared sentinel that stub adapters return.
var ErrNotConfigured = errors.New("search: adapter not configured")

// Result is one search hit. URL is canonical; Title and Snippet are
// the strings the agent uses for reasoning. Score is optional — some
// backends provide a relevance score, others don't.
type Result struct {
	URL     string  `json:"url"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet,omitempty"`
	Score   float64 `json:"score,omitempty"`
}

// Adapter is the per-backend contract. Search is stateless — there's
// only one verb (Search) and no session id.
type Adapter interface {
	// ID returns the adapter identifier ("searxng" / "tavily" / etc.).
	ID() string

	// Configured reports whether the backing service is ready.
	Configured() bool

	// Search executes one query. maxResults is a soft cap; backends
	// may return fewer (no results) or be capped lower internally.
	Search(ctx context.Context, query string, maxResults int) ([]Result, error)
}
