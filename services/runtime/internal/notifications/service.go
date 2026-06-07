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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service wires an adapter, a recorder, and a logger into the
// public Send / List / Get / Stats surface the REST handlers consume.
type Service struct {
	adapter  Adapter
	recorder *Recorder
	log      *slog.Logger
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
		adapter:  adapter,
		recorder: rec,
		log:      log,
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
