// SPDX-License-Identifier: Apache-2.0

// Package none implements the zero-config metrics adapter.
package none

import (
	"context"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/metrics"
)

type Store struct{}

var _ metrics.Store = Store{}

func New() Store { return Store{} }

func (Store) Query(context.Context, string, time.Time) ([]metrics.InstantSample, error) {
	return nil, metrics.ErrNoBackend
}

func (Store) QueryRange(context.Context, string, time.Time, time.Time, time.Duration) ([]metrics.RangeSample, error) {
	return nil, metrics.ErrNoBackend
}

func (Store) Capabilities() metrics.Capabilities {
	return metrics.Capabilities{}
}
