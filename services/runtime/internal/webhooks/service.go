// SPDX-License-Identifier: Apache-2.0

// service.go — top-level Service facade for the webhook package.
//
// The package has TWO data sources after OSS-AUDIT item #2:
//
//   - INBOUND: still lives in our Postgres (suite_webhook_deliveries).
//     The InboundService writes rows; the DeliveryStore reads them.
//
//   - OUTBOUND: lives in Svix (sidecar Docker container). The
//     OutboundService is a thin HTTP proxy that translates the Svix wire
//     format back to our Delivery struct so the dashboard sees no drift.
//
// This file is the seam: callers reach into Service.List / Service.Get /
// Service.Enqueue / Service.Retry without caring which side answers.
// When the caller filters by direction, we route to one source; when
// they don't filter, we merge.
package webhooks

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// Service is the dashboard-facing facade. It composes the per-surface
// services so the HTTP layer routes every webhook verb through one
// dependency.
type Service struct {
	store         *DeliveryStore // inbound rows
	outbound      Outbound       // native (default) or Svix proxy (opt-in)
	endpoints     *EndpointStore
	inbound       *InboundService
	subscriptions *SubscriptionStore // tenant-owned outbound subscribers
	log           *slog.Logger
}

// WithSubscriptions attaches the tenant subscription store. Kept as a
// setter (rather than a NewService param) so existing callers/tests that
// don't use the subscribe+emit surface compile unchanged.
func (s *Service) WithSubscriptions(store *SubscriptionStore) *Service {
	if s == nil {
		return nil
	}
	s.subscriptions = store
	return s
}

// Subscriptions exposes the subscription store (nil when not wired).
func (s *Service) Subscriptions() *SubscriptionStore {
	if s == nil {
		return nil
	}
	return s.subscriptions
}

// Emit fans out an event to the caller tenant's active subscriptions as
// native, per-subscription-signed deliveries. Returns the number enqueued
// and their delivery ids. Zero subscribers is not an error (returns 0).
// The caller must pass the resolved tenant id (used to stamp deliveries);
// the subscription lookup itself is RLS-scoped by ctx.
func (s *Service) Emit(ctx context.Context, tenantID, eventType string, body []byte) (int, []string, error) {
	if s == nil || s.subscriptions == nil || !s.subscriptions.HasPool() {
		return 0, nil, ErrNotConfigured
	}
	if s.outbound == nil || !s.outbound.Configured() {
		return 0, nil, ErrNotConfigured
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return 0, nil, fmt.Errorf("%w: event_type is required", ErrInvalidInput)
	}
	if len(body) == 0 {
		body = []byte("null")
	}
	subs, err := s.subscriptions.ActiveForEvent(ctx, eventType)
	if err != nil {
		return 0, nil, err
	}
	ids := make([]string, 0, len(subs))
	for _, sub := range subs {
		headers := map[string]string{
			HeaderSignature: "sha256=" + SignBody(sub.Secret, body),
		}
		d, err := s.outbound.Enqueue(ctx, SendInput{
			URL:       sub.URL,
			EventType: eventType,
			Body:      body,
			Headers:   headers,
			TenantID:  tenantID,
		})
		if err != nil {
			// One bad subscriber shouldn't sink the whole emit; log + skip.
			s.log.Warn("webhooks: emit enqueue failed for subscriber",
				"subscription_id", sub.ID, "error", err)
			continue
		}
		ids = append(ids, d.ID)
	}
	return len(ids), ids, nil
}

// NewService composes the facade. Every argument may be nil — a nil
// outbound proxy leaves Configured() false so the HTTP layer surfaces
// 503 on /send + /retry; a nil endpoints leaves Endpoints() == nil so
// the inbound CRUD endpoints surface 503; a nil store leaves the
// inbound deliveries list empty.
func NewService(
	store *DeliveryStore,
	outbound Outbound,
	endpoints *EndpointStore,
	inbound *InboundService,
	log *slog.Logger,
) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		store:     store,
		outbound:  outbound,
		endpoints: endpoints,
		inbound:   inbound,
		log:       log,
	}
}

// Configured reports whether the outbound (Svix) surface can accept a
// send. The inbound surface has its own Configured() check; callers
// that need it should reach through Service.Inbound() and call there.
func (s *Service) Configured() bool {
	return s != nil && s.outbound != nil && s.outbound.Configured()
}

// Outbound exposes the outbound delivery backend. Useful for tests; the
// HTTP layer goes through Service.Enqueue / Service.Retry / Service.List.
func (s *Service) Outbound() Outbound {
	if s == nil {
		return nil
	}
	return s.outbound
}

// Endpoints exposes the EndpointStore for the inbound CRUD handlers.
// nil when inbound is not wired.
func (s *Service) Endpoints() *EndpointStore {
	if s == nil {
		return nil
	}
	return s.endpoints
}

// Inbound exposes the InboundService for the public /webhooks/in/{slug}
// receiver. nil when inbound is not wired.
func (s *Service) Inbound() *InboundService {
	if s == nil {
		return nil
	}
	return s.inbound
}

// Store exposes the underlying DeliveryStore for advanced callers (the
// inbound handler needs it directly when it lands rows for unknown
// endpoints during dry-run mode). Post-Svix migration, the store holds
// ONLY inbound rows.
func (s *Service) Store() *DeliveryStore {
	if s == nil {
		return nil
	}
	return s.store
}

// Enqueue forwards to the OutboundService (Svix proxy).
func (s *Service) Enqueue(ctx context.Context, in SendInput) (*Delivery, error) {
	if s == nil || s.outbound == nil {
		return nil, ErrNotConfigured
	}
	return s.outbound.Enqueue(ctx, in)
}

// Retry forwards to the OutboundService.
func (s *Service) Retry(ctx context.Context, id string) (*Delivery, error) {
	if s == nil || s.outbound == nil {
		return nil, ErrNotConfigured
	}
	return s.outbound.Retry(ctx, id)
}

// List returns a page of deliveries matching the filters. The data
// source depends on the direction filter:
//
//   - f.Direction == "inbound"  -> the local DeliveryStore.
//   - f.Direction == "outbound" -> Svix (via OutboundService).
//   - f.Direction == ""         -> both, merged by created_at desc.
//
// Returns an empty page (not 503) when nothing is wired so the
// dashboard renders the empty state.
func (s *Service) List(ctx context.Context, f ListFilters) (*ListResult, error) {
	if s == nil {
		return &ListResult{Deliveries: []Delivery{}}, nil
	}

	switch f.Direction {
	case DirectionInbound:
		if s.store == nil {
			return &ListResult{Deliveries: []Delivery{}}, nil
		}
		return s.store.List(ctx, f)
	case DirectionOutbound:
		if s.outbound == nil {
			return &ListResult{Deliveries: []Delivery{}}, nil
		}
		return s.outbound.List(ctx, f)
	}

	// Unified list: pull from both sources and merge. We split the
	// limit roughly down the middle so neither side starves the other.
	half := f.Limit
	if half <= 0 {
		half = 50
	}
	inFilter := f
	inFilter.Direction = DirectionInbound
	inFilter.Limit = half
	outFilter := f
	outFilter.Direction = DirectionOutbound
	outFilter.Limit = half

	var (
		inboundRes  = &ListResult{Deliveries: []Delivery{}}
		outboundRes = &ListResult{Deliveries: []Delivery{}}
		err         error
	)
	if s.store != nil {
		inboundRes, err = s.store.List(ctx, inFilter)
		if err != nil {
			return nil, err
		}
	}
	if s.outbound != nil {
		outboundRes, err = s.outbound.List(ctx, outFilter)
		if err != nil {
			// Don't fail the whole list if Svix is briefly down — show
			// inbound anyway so the dashboard isn't blank.
			s.log.Warn("webhooks: outbound list failed; serving inbound only",
				"error", err)
			outboundRes = &ListResult{Deliveries: []Delivery{}}
		}
	}

	merged := make([]Delivery, 0, len(inboundRes.Deliveries)+len(outboundRes.Deliveries))
	merged = append(merged, inboundRes.Deliveries...)
	merged = append(merged, outboundRes.Deliveries...)
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].CreatedAt > merged[j].CreatedAt
	})
	if f.Limit > 0 && len(merged) > f.Limit {
		merged = merged[:f.Limit]
	}
	return &ListResult{
		Deliveries: merged,
		Total:      inboundRes.Total + outboundRes.Total,
		HasMore:    inboundRes.HasMore || outboundRes.HasMore,
	}, nil
}

// Get loads a single delivery by ID. We try the outbound (Svix) side
// first because Svix message IDs are the common case for retry-from-
// dashboard flows; if Svix returns not-found we fall through to the
// local inbound store.
func (s *Service) Get(ctx context.Context, id string) (*Delivery, error) {
	if s == nil {
		return nil, ErrNotFound
	}
	if s.outbound != nil && s.outbound.Configured() {
		d, err := s.outbound.Get(ctx, id)
		if err == nil {
			return d, nil
		}
		// Fall through on not-found; surface other errors.
		if err != ErrNotFound {
			s.log.Debug("webhooks: outbound get miss",
				"id", id, "error", err)
		}
	}
	if s.store != nil {
		return s.store.Get(ctx, id)
	}
	return nil, ErrNotFound
}
