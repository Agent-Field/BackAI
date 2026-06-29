// SPDX-License-Identifier: Apache-2.0

// Package empty implements the zero-config traces.Store.
package empty

import (
	"context"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/traces"
)

type Store struct{}

var _ traces.Store = Store{}

func New() Store {
	return Store{}
}

func (Store) Search(ctx context.Context, _ traces.SearchFilter) (traces.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return traces.SearchResult{}, err
	}
	return traces.SearchResult{Traces: []traces.TraceSummary{}}, nil
}

func (Store) Get(ctx context.Context, traceID string) (traces.Trace, error) {
	if err := ctx.Err(); err != nil {
		return traces.Trace{}, err
	}
	if strings.TrimSpace(traceID) == "" {
		return traces.Trace{}, traces.ErrInvalidTraceID
	}
	return traces.Trace{}, traces.ErrNoBackend
}

func (Store) Capabilities() traces.Capabilities {
	return traces.Capabilities{}
}
