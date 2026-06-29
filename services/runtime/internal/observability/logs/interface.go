// SPDX-License-Identifier: Apache-2.0

// Package logs defines the runtime log-store adapter contract.
package logs

import (
	"context"
	"errors"
	"time"
)

var ErrUnsupportedCapability = errors.New("unsupported_capability")

// Entry is one normalized log line. JSON field names intentionally match the
// dashboard's LogLineSchema: use msg, not message, and preserve agent.
type Entry struct {
	TS        time.Time      `json:"ts"`
	Level     string         `json:"level"`
	Service   string         `json:"service"`
	Msg       string         `json:"msg"`
	Agent     string         `json:"agent,omitempty"`
	TenantID  string         `json:"tenant_id,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	TraceID   string         `json:"trace_id,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// Filter is the admin-facing query. Empty slices mean "all".
type Filter struct {
	Services      []string
	Levels        []string
	TenantID      string
	RequestID     string
	TraceID       string
	Search        string
	SearchIsRegex bool
	From          time.Time
	To            time.Time
	Limit         int
	Cursor        string
}

// Page is one window of entries.
type Page struct {
	Entries    []Entry
	NextCursor string
	HasMore    bool
}

// Capabilities describes an active logs adapter.
type Capabilities struct {
	SupportsTail        bool   `json:"supports_tail"`
	SupportsFullText    bool   `json:"supports_full_text"`
	SupportsRegexSearch bool   `json:"supports_regex_search"`
	SupportsTraceID     bool   `json:"supports_trace_id"`
	NativeQueryLang     string `json:"native_query_lang"`
	RetentionDays       int    `json:"retention_days"`
	MaxEntriesPerPage   int    `json:"max_entries_per_page"`
}

// Store is the logs adapter contract. Implementations must honor context
// cancellation and must not buffer unbounded tail streams in memory.
type Store interface {
	Query(ctx context.Context, f Filter) (Page, error)
	Tail(ctx context.Context, f Filter) (<-chan Entry, error)
	Capabilities() Capabilities
}
