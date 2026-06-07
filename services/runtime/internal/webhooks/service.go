// SPDX-License-Identifier: Apache-2.0

// service.go — top-level Service facade for the webhook package.
//
// CO-OWNED. Phase 10.2 adds the inbound surface (EndpointStore +
// InboundService); Phase 10.3 contributes the outbound surface
// (OutboundService driven by the background worker). The Service binds
// every surface behind one struct so the runtime's server.Deps keeps a
// single field for the whole package — the dashboard's unified
// deliveries feed becomes a single call into Service.List().
//
// The facade is intentionally thin: every method either delegates to
// the DeliveryStore (for reads) or to the OutboundService / EndpointStore
// (for mutating writes). The inbound HTTP handler reaches its dependency
// through Service.Inbound() and never short-circuits the facade.
package webhooks

import (
	"context"
	"log/slog"
)

// Service is the dashboard-facing facade. It composes the shared
// DeliveryStore with the per-surface services so the HTTP layer can
// route every webhook verb through one dependency without growing
// server.Deps with parallel fields.
type Service struct {
	store     *DeliveryStore
	outbound  *OutboundService
	endpoints *EndpointStore
	inbound   *InboundService
	log       *slog.Logger
}

// NewService composes the facade. Every argument may be nil — a nil
// outbound leaves Configured() false so the HTTP layer surfaces 503 on
// /send + /retry; a nil endpoints leaves Endpoints() == nil so the
// inbound CRUD endpoints surface 503; a nil store leaves the list +
// get endpoints serving an empty page.
func NewService(
	store *DeliveryStore,
	outbound *OutboundService,
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

// Configured reports whether the outbound surface (the only mutating
// path Phase 10.3 owns) can write deliveries. The inbound surface has
// its own Configured() check; callers that need it should reach
// through Service.Inbound() and call there.
func (s *Service) Configured() bool {
	return s != nil && s.outbound != nil && s.outbound.Configured()
}

// Outbound exposes the OutboundService for the worker (which only
// needs the outbound surface).
func (s *Service) Outbound() *OutboundService {
	if s == nil {
		return nil
	}
	return s.outbound
}

// Endpoints exposes the EndpointStore for the inbound CRUD handlers.
// nil when Phase 10.2's inbound module isn't wired.
func (s *Service) Endpoints() *EndpointStore {
	if s == nil {
		return nil
	}
	return s.endpoints
}

// Inbound exposes the InboundService for the public /webhooks/in/{slug}
// receiver. nil when Phase 10.2's inbound module isn't wired.
func (s *Service) Inbound() *InboundService {
	if s == nil {
		return nil
	}
	return s.inbound
}

// Store exposes the underlying DeliveryStore for advanced callers (the
// inbound handler needs it directly when it lands rows for unknown
// endpoints during dry-run mode).
func (s *Service) Store() *DeliveryStore {
	if s == nil {
		return nil
	}
	return s.store
}

// Enqueue forwards to the OutboundService.
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

// List returns a page of deliveries matching the filters. Returns an
// empty page (not 503) when no store is wired so the dashboard renders
// the empty state.
func (s *Service) List(ctx context.Context, f ListFilters) (*ListResult, error) {
	if s == nil || s.store == nil {
		return &ListResult{Deliveries: []Delivery{}}, nil
	}
	return s.store.List(ctx, f)
}

// Get loads a single delivery row.
func (s *Service) Get(ctx context.Context, id string) (*Delivery, error) {
	if s == nil || s.store == nil {
		return nil, ErrNotFound
	}
	return s.store.Get(ctx, id)
}