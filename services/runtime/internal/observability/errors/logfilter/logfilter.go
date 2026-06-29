// SPDX-License-Identifier: Apache-2.0

// Package logfilter groups error-level logs from the active logs adapter into
// volatile error groups. It is the zero-config errors adapter.
package logfilter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	obserrors "github.com/Agent-Field/backai/services/runtime/internal/observability/errors"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
)

const (
	defaultLimit      = 50
	maxGroupsPerPage  = 500
	defaultQueryLimit = 1000
	cacheTTL          = 30 * time.Second
)

var (
	hexLikeRE = regexp.MustCompile(`\b[0-9a-fA-F]{8,}\b`)
	uuidRE    = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	numberRE  = regexp.MustCompile(`\b\d+\b`)
	spaceRE   = regexp.MustCompile(`\s+`)
)

type Store struct {
	logs logs.Store
	now  func() time.Time

	mu        sync.Mutex
	overrides map[string]string
	cache     []obserrors.Group
	cacheAt   time.Time
}

var _ obserrors.Store = (*Store)(nil)

func New(logStore logs.Store) *Store {
	return &Store{
		logs:      logStore,
		now:       time.Now,
		overrides: make(map[string]string),
	}
}

func (s *Store) ListGroups(ctx context.Context, f obserrors.ListFilter) (obserrors.Page, error) {
	if s == nil || s.logs == nil {
		return obserrors.Page{Groups: []obserrors.Group{}}, nil
	}
	status := ""
	if strings.TrimSpace(f.Status) != "" {
		var err error
		status, err = obserrors.NormalizeStatus(f.Status)
		if err != nil {
			return obserrors.Page{}, err
		}
	}
	groups, err := s.groups(ctx, f)
	if err != nil {
		return obserrors.Page{}, err
	}
	filtered := make([]obserrors.Group, 0, len(groups))
	for _, group := range groups {
		if status != "" && group.Status != status {
			continue
		}
		if f.Service != "" && !strings.EqualFold(group.Service, f.Service) {
			continue
		}
		filtered = append(filtered, group)
	}
	offset := parseCursor(f.Cursor)
	limit := normalizeLimit(f.Limit)
	if offset >= len(filtered) {
		return obserrors.Page{Groups: []obserrors.Group{}}, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	page := filtered[offset:end]
	out := obserrors.Page{Groups: page}
	if end < len(filtered) {
		out.HasMore = true
		out.NextCursor = strconv.Itoa(end)
	}
	return out, nil
}

func (s *Store) GetGroup(ctx context.Context, groupID string) (obserrors.Group, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return obserrors.Group{}, obserrors.ErrInvalidGroupIdentifier
	}
	groups, err := s.groups(ctx, obserrors.ListFilter{})
	if err != nil {
		return obserrors.Group{}, err
	}
	for _, group := range groups {
		if group.ID == groupID {
			return group, nil
		}
	}
	return obserrors.Group{}, obserrors.ErrGroupNotFound
}

func (s *Store) UpdateGroup(ctx context.Context, groupID string, update obserrors.Update) (obserrors.Group, error) {
	if err := ctx.Err(); err != nil {
		return obserrors.Group{}, err
	}
	status, err := obserrors.NormalizeStatus(update.Status)
	if err != nil {
		return obserrors.Group{}, err
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return obserrors.Group{}, obserrors.ErrInvalidGroupIdentifier
	}
	group, err := s.GetGroup(ctx, groupID)
	if err != nil {
		return obserrors.Group{}, err
	}
	s.mu.Lock()
	s.overrides[groupID] = status
	s.mu.Unlock()
	group.Status = status
	return group, nil
}

func (s *Store) Capabilities() obserrors.Capabilities {
	return obserrors.Capabilities{
		SupportsList:     true,
		SupportsGet:      true,
		SupportsMute:     true,
		SupportsResolve:  true,
		SupportsIngest:   false,
		SupportsAlerting: false,
		NativeQueryLang:  "log-filter",
		RetentionDays:    0,
		Persistence:      obserrors.PersistenceVolatile,
		MaxGroupsPerPage: maxGroupsPerPage,
	}
}

func (s *Store) groups(ctx context.Context, f obserrors.ListFilter) ([]obserrors.Group, error) {
	now := s.now()
	s.mu.Lock()
	if len(s.cache) > 0 && now.Sub(s.cacheAt) < cacheTTL {
		out := cloneGroups(s.cache)
		applyOverrides(out, s.overrides)
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()

	page, err := s.logs.Query(ctx, logs.Filter{
		Levels:   []string{"error", "fatal"},
		TenantID: f.TenantID,
		From:     f.Since,
		Limit:    defaultQueryLimit,
	})
	if err != nil {
		return nil, err
	}
	grouped := map[string]*obserrors.Group{}
	for _, entry := range page.Entries {
		if entry.Level != "error" && entry.Level != "fatal" {
			continue
		}
		fingerprint := fingerprint(entry)
		group := grouped[fingerprint]
		if group == nil {
			group = &obserrors.Group{
				ID:          "logfilter:" + fingerprint,
				Title:       titleForEntry(entry),
				Service:     defaultString(entry.Service, "runtime"),
				Status:      obserrors.StatusOpen,
				FirstSeen:   entry.TS.UTC(),
				LastSeen:    entry.TS.UTC(),
				Fingerprint: fingerprint,
				SampleEvent: map[string]any{
					"ts":         entry.TS.UTC().Format(time.RFC3339Nano),
					"level":      entry.Level,
					"service":    entry.Service,
					"msg":        entry.Msg,
					"agent":      entry.Agent,
					"tenant_id":  entry.TenantID,
					"request_id": entry.RequestID,
					"trace_id":   entry.TraceID,
					"fields":     entry.Fields,
				},
			}
			grouped[fingerprint] = group
		}
		group.Count++
		if entry.TS.Before(group.FirstSeen) {
			group.FirstSeen = entry.TS.UTC()
		}
		if entry.TS.After(group.LastSeen) {
			group.LastSeen = entry.TS.UTC()
		}
	}
	out := make([]obserrors.Group, 0, len(grouped))
	for _, group := range grouped {
		out = append(out, *group)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastSeen.After(out[j].LastSeen)
	})

	s.mu.Lock()
	s.cache = cloneGroups(out)
	s.cacheAt = now
	applyOverrides(out, s.overrides)
	s.mu.Unlock()
	return out, nil
}

func fingerprint(entry logs.Entry) string {
	basis := defaultString(entry.Service, "runtime") + "\n"
	if frame := topStackFrame(entry); frame != "" {
		basis += frame
	} else {
		basis += normalizeMessage(entry.Msg)
	}
	sum := sha256.Sum256([]byte(basis))
	return hex.EncodeToString(sum[:])[:16]
}

func topStackFrame(entry logs.Entry) string {
	for _, key := range []string{"stack", "stacktrace", "exception.stacktrace"} {
		if frame := firstFrame(entry.Fields[key]); frame != "" {
			return frame
		}
	}
	lines := strings.Split(entry.Msg, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "at ") || strings.Contains(line, ".go:") || strings.Contains(line, ".py:") {
			return line
		}
	}
	return ""
}

func firstFrame(v any) string {
	switch t := v.(type) {
	case string:
		for _, line := range strings.Split(t, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				return line
			}
		}
	case []any:
		for _, item := range t {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				return s
			}
		}
	case []string:
		for _, item := range t {
			if s := strings.TrimSpace(item); s != "" {
				return s
			}
		}
	}
	return ""
}

func normalizeMessage(msg string) string {
	msg = strings.ToLower(msg)
	msg = uuidRE.ReplaceAllString(msg, "?")
	msg = hexLikeRE.ReplaceAllString(msg, "?")
	msg = numberRE.ReplaceAllString(msg, "?")
	msg = spaceRE.ReplaceAllString(strings.TrimSpace(msg), " ")
	if len(msg) > 96 {
		msg = msg[:96]
	}
	return msg
}

func titleForEntry(entry logs.Entry) string {
	msg := strings.TrimSpace(strings.Split(entry.Msg, "\n")[0])
	if msg == "" {
		return "Runtime error"
	}
	if len(msg) > 140 {
		return msg[:140]
	}
	return msg
}

func applyOverrides(groups []obserrors.Group, overrides map[string]string) {
	for i := range groups {
		if status := overrides[groups[i].ID]; status != "" {
			groups[i].Status = status
		}
	}
}

func cloneGroups(groups []obserrors.Group) []obserrors.Group {
	out := make([]obserrors.Group, len(groups))
	copy(out, groups)
	return out
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxGroupsPerPage {
		return maxGroupsPerPage
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

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
