// SPDX-License-Identifier: Apache-2.0

// service.go — orchestrates send + persistence + worker hand-off.
//
// Flow for Service.Send:
//
//  1. Validate the input (Template + To bounds, Kind enum). Bad input -> 400.
//  2. Insert the suite_notifications row at status='queued' so the
//     dashboard can see the row immediately.
//  3. The worker (worker.go) eventually picks it up; for "send now"
//     callers that want synchronous semantics we offer an explicit
//     Service.SendSync that dispatches inline and updates the row in
//     one round-trip.
//
// Returning the row from Send (not the eventual delivery result) gives
// callers a handle they can poll via Get / surface to the user. The
// dashboard's "send notification" dialog uses this — the modal closes
// the moment the row is queued, and the table refreshes when the
// worker transitions it.
package notifications

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service wires an adapter, a recorder, and a logger into the
// public Send / List / Get / Stats surface the REST handlers consume.
type Service struct {
	adapter  Adapter
	recorder *Recorder
	log      *slog.Logger

	muteMu    sync.Mutex
	muteCache map[string]muteCacheEntry
	muteTTL   time.Duration
}

type muteCacheEntry struct {
	expiresAt time.Time
	mutes     []Mute
}

// NewService constructs a Service. adapter is required (use the log
// adapter as a safe default in dev). pool may be nil — recorder=nil
// downgrades to "no persistence" and Send returns a synthetic in-memory
// row so callers see something.
func NewService(pool *pgxpool.Pool, adapter Adapter, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	rec := NewRecorder(pool, log)
	return &Service{
		adapter:   adapter,
		recorder:  rec,
		log:       log,
		muteCache: map[string]muteCacheEntry{},
		muteTTL:   5 * time.Second,
	}
}

// AdapterAvailable reports whether the Service has a working backend.
// Used by the REST layer to translate "no adapter wired" into 503.
func (s *Service) AdapterAvailable() bool {
	return s != nil && s.adapter != nil
}

// AdapterName returns the configured adapter's identifier or "" when
// none is wired. Useful for the dashboard's status row.
func (s *Service) AdapterName() string {
	if s == nil || s.adapter == nil {
		return ""
	}
	return s.adapter.Name()
}

// Recorder exposes the persistence layer to the worker goroutine.
func (s *Service) Recorder() *Recorder {
	if s == nil {
		return nil
	}
	return s.recorder
}

// Adapter exposes the configured adapter to the worker goroutine.
func (s *Service) Adapter() Adapter {
	if s == nil {
		return nil
	}
	return s.adapter
}

// Send validates + persists a notification. Returns the queued row.
// The actual delivery happens asynchronously in the worker.
func (s *Service) Send(ctx context.Context, in SendInput) (*Notification, error) {
	if s == nil || s.adapter == nil {
		return nil, ErrNotConfigured
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}
	if s.recorder != nil && s.recorder.HasPool() {
		mute, err := s.matchingMute(ctx, in)
		if err != nil {
			return nil, err
		}
		if mute != nil {
			s.log.Info("notification muted",
				"mute_id", mute.ID,
				"kind", in.Kind,
				"template", in.Template,
				"to", in.To)
			return s.recorder.InsertSkipped(ctx, in, "muted by rule "+mute.ID)
		}
		return s.recorder.Insert(ctx, in)
	}
	// No DB wired: synthesise an ephemeral row so callers see a
	// sensible response. This is dev-only — the worker won't pick it up
	// because nothing was persisted.
	now := time.Now().UTC()
	id := fmt.Sprintf("eph-%d", now.UnixNano())
	var tenantPtr, fromPtr, subjectPtr *string
	if in.TenantID != "" {
		t := in.TenantID
		tenantPtr = &t
	}
	if in.From != "" {
		f := in.From
		fromPtr = &f
	}
	if in.Subject != "" {
		su := in.Subject
		subjectPtr = &su
	}
	return &Notification{
		ID:          id,
		TenantID:    tenantPtr,
		Kind:        in.Kind,
		Template:    in.Template,
		To:          in.To,
		From:        fromPtr,
		Subject:     subjectPtr,
		Data:        cloneData(in.Data),
		Status:      StatusQueued,
		ScheduledAt: now.Format(time.RFC3339Nano),
		CreatedAt:   now.Format(time.RFC3339Nano),
	}, nil
}

func (s *Service) ListMutes(ctx context.Context, tenantID string) (*MuteListResult, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if s.recorder == nil {
		return &MuteListResult{Mutes: []Mute{}}, nil
	}
	return s.recorder.ListMutes(ctx, tenantID)
}

func (s *Service) CreateMute(ctx context.Context, in CreateMuteInput) (*Mute, error) {
	if s == nil || s.recorder == nil || !s.recorder.HasPool() {
		return nil, ErrNotConfigured
	}
	out, err := s.recorder.CreateMute(ctx, in)
	if err == nil {
		s.clearMuteCache()
	}
	return out, err
}

func (s *Service) DeleteMute(ctx context.Context, id string) error {
	if s == nil || s.recorder == nil || !s.recorder.HasPool() {
		return ErrNotConfigured
	}
	err := s.recorder.DeleteMute(ctx, id)
	if err == nil {
		s.clearMuteCache()
	}
	return err
}

func (s *Service) matchingMute(ctx context.Context, in SendInput) (*Mute, error) {
	if s == nil || s.recorder == nil || !s.recorder.HasPool() {
		return nil, nil
	}
	cacheKey := in.TenantID
	now := time.Now()
	s.muteMu.Lock()
	entry, ok := s.muteCache[cacheKey]
	if ok && now.Before(entry.expiresAt) {
		mutes := append([]Mute{}, entry.mutes...)
		s.muteMu.Unlock()
		return firstMatchingMute(mutes, in, now, true), nil
	}
	s.muteMu.Unlock()

	list, err := s.recorder.ListMutes(ctx, in.TenantID)
	if err != nil {
		return nil, err
	}
	mutes := []Mute{}
	if list != nil {
		mutes = append(mutes, list.Mutes...)
	}
	s.muteMu.Lock()
	s.muteCache[cacheKey] = muteCacheEntry{expiresAt: now.Add(s.muteTTL), mutes: append([]Mute{}, mutes...)}
	s.muteMu.Unlock()
	return firstMatchingMute(mutes, in, now, true), nil
}

func (s *Service) clearMuteCache() {
	if s == nil {
		return
	}
	s.muteMu.Lock()
	defer s.muteMu.Unlock()
	s.muteCache = map[string]muteCacheEntry{}
}

func firstMatchingMute(mutes []Mute, in SendInput, now time.Time, preferTenantSpecific bool) *Mute {
	if preferTenantSpecific {
		if m := firstMatchingMute(mutes, in, now, false); m != nil && m.TenantID != nil {
			return m
		}
		for i := range mutes {
			if mutes[i].TenantID != nil {
				continue
			}
			if muteMatches(&mutes[i], in, now) {
				return &mutes[i]
			}
		}
		return nil
	}
	for i := range mutes {
		if mutes[i].TenantID == nil {
			continue
		}
		if muteMatches(&mutes[i], in, now) {
			return &mutes[i]
		}
	}
	return nil
}

func muteMatches(m *Mute, in SendInput, now time.Time) bool {
	category := "*"
	if v, ok := in.Data["category"].(string); ok && strings.TrimSpace(v) != "" {
		category = strings.TrimSpace(v)
	}
	if m.ExpiresAt != nil {
		expiresAt, err := time.Parse(time.RFC3339Nano, *m.ExpiresAt)
		if err == nil && !expiresAt.After(now) {
			return false
		}
	}
	return muteFieldMatches(m.Pattern.Kind, string(in.Kind)) &&
		muteFieldMatches(m.Pattern.Recipient, in.To) &&
		muteFieldMatches(m.Pattern.Template, in.Template) &&
		muteFieldMatches(m.Pattern.Category, category)
}

func muteFieldMatches(pattern, value string) bool {
	return pattern == "*" || pattern == value
}

// List returns one page of notifications.
func (s *Service) List(ctx context.Context, f ListFilters) (*ListResult, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if s.recorder == nil {
		return &ListResult{Notifications: []Notification{}}, nil
	}
	return s.recorder.List(ctx, f)
}

// Get fetches a single notification by id.
func (s *Service) Get(ctx context.Context, id string) (*Notification, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if s.recorder == nil {
		return nil, ErrNotFound
	}
	return s.recorder.Get(ctx, id)
}

// Stats returns aggregate KPIs.
func (s *Service) Stats(ctx context.Context, tenantID string) (*Stats, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if s.recorder == nil {
		return &Stats{
			ByStatus:  map[Status]int{},
			ByAdapter: []AdapterCount{},
		}, nil
	}
	return s.recorder.Stats(ctx, tenantID)
}

// dispatchOne runs one notification through the adapter and updates
// the row's terminal status. Used by the worker.
func (s *Service) dispatchOne(ctx context.Context, n Notification) {
	if s == nil || s.adapter == nil || s.recorder == nil {
		return
	}
	pmID, err := s.adapter.Send(ctx, n)
	if err != nil {
		// Note: we don't auto-retry in v1. Operators flip the row back
		// to 'queued' via SQL (or a future retry endpoint) when ready.
		if updateErr := s.recorder.UpdateStatus(ctx, n.ID,
			StatusFailed, s.adapter.Name(), "", err.Error(),
		); updateErr != nil {
			s.log.Warn("notifications: mark failed",
				"id", n.ID, "error", updateErr)
		}
		return
	}
	if updateErr := s.recorder.UpdateStatus(ctx, n.ID,
		StatusSent, s.adapter.Name(), pmID, "",
	); updateErr != nil {
		s.log.Warn("notifications: mark sent",
			"id", n.ID, "error", updateErr)
	}
}

// Keep errors.Is alive across refactors.
var _ = errors.Is
