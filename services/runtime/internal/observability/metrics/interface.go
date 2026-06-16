// SPDX-License-Identifier: Apache-2.0

// Package metrics defines the runtime metrics adapter contract.
package metrics

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNoBackend          = errors.New("metrics_no_backend")
	ErrUnsupportedShape   = errors.New("unsupported_result_shape")
	ErrUnsupportedBackend = errors.New("unsupported_metrics_backend")
)

// InstantSample is one Prometheus vector sample normalized for admin APIs.
type InstantSample struct {
	Metric map[string]string `json:"metric"`
	Value  float64           `json:"value"`
	TS     time.Time         `json:"ts"`
}

// TimedValue is one timestamped sample in a range query matrix.
type TimedValue struct {
	TS    time.Time `json:"ts"`
	Value float64   `json:"value"`
}

// RangeSample is one Prometheus matrix series normalized for admin APIs.
type RangeSample struct {
	Metric map[string]string `json:"metric"`
	Values []TimedValue      `json:"values"`
}

// Capabilities describes the active metrics adapter.
type Capabilities struct {
	SupportsInstantQuery bool   `json:"supports_instant_query"`
	SupportsRangeQuery   bool   `json:"supports_range_query"`
	SupportsContainer    bool   `json:"supports_container_metrics"`
	NativeQueryLang      string `json:"native_query_lang"`
	RetentionHours       int    `json:"retention_hours"`
	MaxSeriesPerQuery    int    `json:"max_series_per_query"`
}

// Store is the metrics adapter contract. Implementations must honor context
// cancellation and keep outbound query calls bounded.
type Store interface {
	Query(ctx context.Context, promQL string, at time.Time) ([]InstantSample, error)
	QueryRange(ctx context.Context, promQL string, from, to time.Time, step time.Duration) ([]RangeSample, error)
	Capabilities() Capabilities
}
