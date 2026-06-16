// SPDX-License-Identifier: Apache-2.0

// Package traces defines the runtime trace-store adapter contract.
package traces

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNoBackend      = errors.New("traces_no_backend")
	ErrTraceNotFound  = errors.New("trace_not_found")
	ErrInvalidTraceID = errors.New("invalid_trace_id")
)

type TraceSummary struct {
	TraceID       string        `json:"trace_id"`
	RootService   string        `json:"root_service"`
	RootOperation string        `json:"root_operation"`
	StartTime     time.Time     `json:"start_time"`
	Duration      time.Duration `json:"duration"`
	SpanCount     int           `json:"span_count"`
	Status        string        `json:"status"`
}

type SearchFilter struct {
	Service     string
	Operation   string
	Tag         map[string]string
	MinDuration time.Duration
	MaxDuration time.Duration
	Status      string
	From        time.Time
	To          time.Time
	Limit       int
}

type SearchResult struct {
	Traces  []TraceSummary `json:"traces"`
	HasMore bool           `json:"has_more"`
}

type Span struct {
	SpanID       string         `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id"`
	Service      string         `json:"service"`
	Operation    string         `json:"operation"`
	StartTime    time.Time      `json:"start_time"`
	Duration     time.Duration  `json:"duration"`
	Status       string         `json:"status"`
	Attributes   map[string]any `json:"attributes"`
	Events       []SpanEvent    `json:"events"`
}

type SpanEvent struct {
	TS         time.Time      `json:"ts"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes"`
}

type Trace struct {
	TraceID string `json:"trace_id"`
	Spans   []Span `json:"spans"`
}

type Capabilities struct {
	SupportsTraceQL    bool   `json:"supports_traceql"`
	SupportsTagSearch  bool   `json:"supports_tag_search"`
	NativeQueryLang    string `json:"native_query_lang"`
	RetentionHours     int    `json:"retention_hours"`
	MaxResultsPerQuery int    `json:"max_results_per_query"`
}

type Store interface {
	Search(ctx context.Context, f SearchFilter) (SearchResult, error)
	Get(ctx context.Context, traceID string) (Trace, error)
	Capabilities() Capabilities
}
