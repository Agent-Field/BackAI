// SPDX-License-Identifier: Apache-2.0

// outbound.go — outbound webhook delivery service.
//
// Phase 10.3 owns this file. The OutboundService is the public surface
// the HTTP handlers + SDK call into:
//
//   - Enqueue(ctx, SendInput) — insert a row, optionally POST it now.
//   - Deliver(ctx, id)        — load the row, POST to destination,
//                               record response / failure on the row.
//   - Retry(ctx, id)          — reset attempts + scheduled_at so the
//                               worker (or operator) can have another go.
//
// The companion worker.go drives Deliver in a background loop. Sync
// delivery (Immediate=true) and worker-driven delivery share the same
// Deliver method so the dashboard sees identical row updates either way.
package webhooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/safehttp"
)

// OutboundService composes the shared DeliveryStore with an HTTP client.
//
// The HTTP client is intentionally not the global http.DefaultClient: we
// set our own per-request timeout, and we never want a global rewrite
// (e.g. a test injecting a roundtripper) to leak into webhook POSTs.
//
// SSRF defence: the client comes from safehttp.New(...) which refuses
// to dial private / link-local / loopback CIDRs and the cloud-metadata
// hostnames. Without this the destination URL (user-supplied) could be
// pointed at 169.254.169.254 to exfiltrate cloud credentials.
type OutboundService struct {
	store  *DeliveryStore
	client *http.Client
	log    *slog.Logger
}

// NewOutboundService constructs an OutboundService. A nil store
// degrades all methods to ErrNotConfigured so the HTTP handlers can
// surface 503 without panicking.
func NewOutboundService(store *DeliveryStore, log *slog.Logger) *OutboundService {
	if log == nil {
		log = slog.Default()
	}
	return &OutboundService{
		store:  store,
		client: safehttp.New(safehttp.Options{Timeout: DefaultTimeout}),
		log:    log,
	}
}

// Configured reports whether the service has a wired DeliveryStore.
// Used by the HTTP layer to translate "no store" into 503.
func (s *OutboundService) Configured() bool {
	return s != nil && s.store.HasPool()
}

// Enqueue inserts a delivery row. When in.Immediate is true the row is
// inserted at status='delivering', POSTed synchronously, and the row is
// updated with the response before returning. Otherwise the row sits at
// status='queued' and the background worker (OutboundWorker.Run) picks
// it up on the next tick.
//
// Returns the rendered Delivery (terminal status when Immediate=true,
// queued otherwise).
func (s *OutboundService) Enqueue(ctx context.Context, in SendInput) (*Delivery, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	if err := validateSendInput(&in); err != nil {
		return nil, err
	}

	var tenantPtr *string
	if in.TenantID != "" {
		t := in.TenantID
		tenantPtr = &t
	}

	initialStatus := StatusQueued
	if in.Immediate {
		initialStatus = StatusDelivering
	}

	row, err := s.store.Insert(ctx, InsertParams{
		TenantID:    tenantPtr,
		Direction:   DirectionOutbound,
		Destination: in.URL,
		EventType:   in.EventType,
		Status:      initialStatus,
		Headers:     in.Headers,
		Body:        in.Body,
		ScheduledAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("webhooks: enqueue insert: %w", err)
	}

	if !in.Immediate {
		return row, nil
	}

	// Synchronous delivery path: POST now, update the row, return the
	// terminal shape. We POST with the same row we just inserted so we
	// don't burn a second round-trip to load it.
	terminal := s.deliverRow(ctx, row)
	if terminal != nil {
		// Best-effort re-read so the response carries the exact
		// columns the dashboard would see.
		fresh, getErr := s.store.Get(ctx, terminal.ID)
		if getErr == nil {
			return fresh, nil
		}
		return terminal, nil
	}
	return row, nil
}

// Deliver loads the row by id and POSTs it to its destination. Used by
// the worker; tests may call it directly for determinism. On success
// the row transitions to succeeded; on failure attempts is incremented
// and scheduled_at is bumped by linear backoff (cap MaxBackoff).
func (s *OutboundService) Deliver(ctx context.Context, deliveryID string) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	row, err := s.store.Get(ctx, deliveryID)
	if err != nil {
		return err
	}
	if row.Direction != DirectionOutbound {
		return fmt.Errorf("%w: delivery %s is %s, cannot Deliver",
			ErrInvalidInput, deliveryID, row.Direction)
	}
	if row.Status.IsTerminal() {
		// Already done — nothing to do; not an error from the
		// worker's POV.
		return nil
	}
	_ = s.deliverRow(ctx, row)
	return nil
}

// Retry resets the row to queued + attempts=0 so the worker picks it
// up on the next tick. Returns the refreshed row.
func (s *OutboundService) Retry(ctx context.Context, deliveryID string) (*Delivery, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	if err := s.store.ResetForRetry(ctx, deliveryID); err != nil {
		return nil, err
	}
	return s.store.Get(ctx, deliveryID)
}

// List forwards to the underlying DeliveryStore. The dashboard's
// unified deliveries feed (both directions) reaches the store through
// this method — the OutboundService is the canonical entrypoint for
// every delivery read so Phase 10.2 + 10.3 don't fight over a parallel
// reader.
//
// Returns an empty page (not an error) when no store is wired so the
// dashboard renders the empty state rather than 503.
func (s *OutboundService) List(ctx context.Context, f ListFilters) (*ListResult, error) {
	if s == nil || s.store == nil {
		return &ListResult{Deliveries: []Delivery{}}, nil
	}
	return s.store.List(ctx, f)
}

// Get forwards to the underlying DeliveryStore. Returns ErrNotFound
// when the id is unknown OR when no store is wired (the HTTP layer
// maps both to 404).
func (s *OutboundService) Get(ctx context.Context, id string) (*Delivery, error) {
	if s == nil || s.store == nil {
		return nil, ErrNotFound
	}
	return s.store.Get(ctx, id)
}

// deliverRow performs the actual POST and updates the row. Returns the
// terminal row pointer (the input mutated) for caller convenience.
//
// We always update the row — even on transport failure — so the
// dashboard never sees a row stuck at status='delivering'.
func (s *OutboundService) deliverRow(ctx context.Context, row *Delivery) *Delivery {
	now := time.Now().UTC()
	attempts := row.Attempts + 1

	respStatus, respMS, errStr := s.doPOST(ctx, row)

	update := UpdateParams{
		Attempts: &attempts,
	}
	if respStatus > 0 {
		v := respStatus
		update.ResponseStatus = &v
	}
	if respMS > 0 {
		v := respMS
		update.ResponseMS = &v
	}
	if errStr != "" {
		v := errStr
		update.LastError = &v
	}

	// Decide terminal state.
	switch {
	case respStatus >= 200 && respStatus < 300:
		update.Status = StatusSucceeded
		d := now
		update.DeliveredAt = &d
	default:
		update.Status = StatusFailed
		if attempts < MaxAttempts {
			next := now.Add(backoffFor(attempts))
			update.ScheduledAt = &next
		}
	}

	if err := s.store.UpdateStatus(ctx, row.ID, update); err != nil {
		s.log.Warn("webhooks: update terminal status failed",
			"id", row.ID, "error", err)
	}

	// Mutate the in-memory copy so the caller (sync path) returns a
	// consistent shape if the re-read fails.
	row.Attempts = attempts
	row.Status = update.Status
	row.ResponseStatus = update.ResponseStatus
	row.ResponseMS = update.ResponseMS
	row.LastError = update.LastError
	if update.DeliveredAt != nil {
		v := update.DeliveredAt.UTC().Format(time.RFC3339Nano)
		row.DeliveredAt = &v
	}
	if update.ScheduledAt != nil {
		row.ScheduledAt = update.ScheduledAt.UTC().Format(time.RFC3339Nano)
	}
	return row
}

// doPOST is the actual network call. Returns (status, latencyMS,
// errorString). A non-empty error string indicates a transport failure
// (status will be 0); a non-2xx response sets status without setting
// the error string.
func (s *OutboundService) doPOST(ctx context.Context, row *Delivery) (int, int, string) {
	postCtx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(postCtx, http.MethodPost,
		row.Destination, bytes.NewReader(row.Body))
	if err != nil {
		return 0, 0, fmt.Sprintf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AF-Stack-Webhooks/0.1")
	req.Header.Set("X-AF-Event-Type", row.EventType)
	for k, v := range row.Headers {
		// Custom headers override defaults — that's the contract;
		// the dashboard / SDK validates upstream.
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := s.client.Do(req)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		return 0, latency, fmt.Sprintf("transport: %v", err)
	}
	defer resp.Body.Close()
	// Drain the body so the underlying connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, latency, fmt.Sprintf("upstream returned %d", resp.StatusCode)
	}
	return resp.StatusCode, latency, ""
}

// validateSendInput enforces the bare minimum: a parseable absolute
// URL with http/https scheme and a non-empty event type. The HTTP
// layer already validates the JSON shape; this is the last line.
func validateSendInput(in *SendInput) error {
	if in == nil {
		return fmt.Errorf("%w: nil input", ErrInvalidInput)
	}
	in.URL = strings.TrimSpace(in.URL)
	in.EventType = strings.TrimSpace(in.EventType)
	if in.URL == "" {
		return fmt.Errorf("%w: url is required", ErrInvalidInput)
	}
	if in.EventType == "" {
		return fmt.Errorf("%w: event_type is required", ErrInvalidInput)
	}
	u, err := url.Parse(in.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%w: url must be absolute http(s)", ErrInvalidInput)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("%w: url scheme must be http or https", ErrInvalidInput)
	}
	if in.Body == nil {
		in.Body = []byte("null")
	}
	return nil
}

// backoffFor returns the delay before the next attempt. Linear with a
// hard cap; deliberately simple — a missed delivery isn't bursty so
// exponential doesn't buy much, and the cap protects against a
// pathological row that keeps failing.
func backoffFor(attempts int) time.Duration {
	d := time.Duration(attempts) * BackoffStep
	if d > MaxBackoff {
		return MaxBackoff
	}
	return d
}

// Ensure error sentinels stay reachable for callers (errors.Is users).
var _ = errors.Is