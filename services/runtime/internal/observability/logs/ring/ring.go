// SPDX-License-Identifier: Apache-2.0

// Package ring adapts the in-process logger.Ring to the logs.Store contract.
package ring

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/logger"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
)

const maxEntriesPerPage = 1000

type Store struct {
	ring *logger.Ring
}

var _ logs.Store = (*Store)(nil)

func New(r *logger.Ring) *Store {
	return &Store{ring: r}
}

func (s *Store) Query(ctx context.Context, f logs.Filter) (logs.Page, error) {
	if err := ctx.Err(); err != nil {
		return logs.Page{}, err
	}
	limit := normalizeLimit(f.Limit)
	if s == nil || s.ring == nil {
		return logs.Page{Entries: []logs.Entry{}}, nil
	}
	offset := parseCursor(f.Cursor)
	lines := s.ring.Recent(logger.RingSize)
	matches := make([]logs.Entry, 0, min(len(lines), limit))
	seen := 0
	for _, line := range lines {
		entry := entryFromLine(line)
		if !matchesFilter(entry, f) {
			continue
		}
		if seen < offset {
			seen++
			continue
		}
		if len(matches) >= limit {
			return logs.Page{
				Entries:    matches,
				NextCursor: strconv.Itoa(offset + len(matches)),
				HasMore:    true,
			}, nil
		}
		matches = append(matches, entry)
	}
	return logs.Page{Entries: matches}, nil
}

func (s *Store) Tail(ctx context.Context, f logs.Filter) (<-chan logs.Entry, error) {
	out := make(chan logs.Entry)
	if s == nil || s.ring == nil {
		close(out)
		return out, nil
	}
	lines := s.ring.Subscribe(ctx)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-lines:
				if !ok {
					return
				}
				entry := entryFromLine(line)
				if !matchesFilter(entry, f) {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case out <- entry:
				}
			}
		}
	}()
	return out, nil
}

func (s *Store) Capabilities() logs.Capabilities {
	return logs.Capabilities{
		SupportsTail:        true,
		SupportsFullText:    true,
		SupportsRegexSearch: false,
		SupportsTraceID:     false,
		NativeQueryLang:     "",
		RetentionDays:       0,
		MaxEntriesPerPage:   maxEntriesPerPage,
	}
}

func entryFromLine(line logger.Line) logs.Entry {
	ts, err := time.Parse(time.RFC3339Nano, line.TS)
	if err != nil {
		ts = time.Now().UTC()
	}
	fields := line.Fields
	traceID := ""
	if fields != nil {
		if v, ok := fields["trace_id"].(string); ok {
			traceID = v
		} else if v, ok := fields["traceID"].(string); ok {
			traceID = v
		}
	}
	return logs.Entry{
		TS:        ts.UTC(),
		Level:     coerceLevel(string(line.Level)),
		Service:   line.Service,
		Msg:       line.Msg,
		Agent:     line.Agent,
		TenantID:  line.TenantID,
		RequestID: line.RequestID,
		TraceID:   traceID,
		Fields:    fields,
	}
}

func matchesFilter(e logs.Entry, f logs.Filter) bool {
	if len(f.Services) > 0 && !contains(f.Services, e.Service) {
		return false
	}
	if len(f.Levels) > 0 && !contains(f.Levels, e.Level) {
		return false
	}
	if f.TenantID != "" && e.TenantID != f.TenantID {
		return false
	}
	if f.RequestID != "" && e.RequestID != f.RequestID {
		return false
	}
	if f.TraceID != "" && e.TraceID != f.TraceID {
		return false
	}
	if !f.From.IsZero() && e.TS.Before(f.From) {
		return false
	}
	if !f.To.IsZero() && e.TS.After(f.To) {
		return false
	}
	if f.Search != "" && !strings.Contains(e.Msg, f.Search) {
		return false
	}
	return true
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 200
	}
	if limit > maxEntriesPerPage {
		return maxEntriesPerPage
	}
	return limit
}

func parseCursor(cursor string) int {
	if cursor == "" {
		return 0
	}
	n, err := strconv.Atoi(cursor)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}

func coerceLevel(level string) string {
	switch strings.ToLower(level) {
	case "debug", "info", "warn", "error", "fatal":
		return strings.ToLower(level)
	default:
		return "info"
	}
}
