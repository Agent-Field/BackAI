// SPDX-License-Identifier: Apache-2.0

// native.go — in-process outbound webhook delivery.
//
// Delivering a webhook is a signed HTTP POST with retries. This runtime
// persists an outbox (suite_webhook_deliveries, direction='outbound', with
// a (direction,status,scheduled_at) index built for exactly this) and knows
// how to retry work, so NativeOutbound drains that outbox in-process: it is
// the sole outbound-delivery implementation — zero-config, no external
// sidecar.
//
// Delivery lifecycle for one row:
//
//	queued --claim--> delivering --POST 2xx--> succeeded
//	                            \--POST !2xx / transport--> failed (retry)
//	                              failed rows are re-claimed once their
//	                              backoff (scheduled_at) elapses, until
//	                              attempts == MaxAttempts (then they rest
//	                              as 'failed', terminal-by-exhaustion).
//
// Signing. Each POST carries X-AF-Webhook-Event + X-AF-Webhook-Timestamp,
// and — when a signing secret is configured — X-AF-Webhook-Signature:
// sha256=<hmac(body)> so subscribers can verify authenticity (the same
// HMAC scheme VerifyHMAC checks on the inbound side).
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/safehttp"
)

// Outbound is the surface the webhook Service facade routes outbound verbs
// through. NativeOutbound is its sole implementation; the interface stays a
// seam so the facade and HTTP handlers depend on behavior, not the concrete
// type (and tests can substitute a fake).
type Outbound interface {
	Enqueue(ctx context.Context, in SendInput) (*Delivery, error)
	Retry(ctx context.Context, deliveryID string) (*Delivery, error)
	List(ctx context.Context, f ListFilters) (*ListResult, error)
	Get(ctx context.Context, deliveryID string) (*Delivery, error)
	Configured() bool
}

// Signature header names. Subscribers verify X-AF-Webhook-Signature with
// the shared secret; the timestamp guards against replay and the event
// header lets a subscriber route without parsing the body.
const (
	HeaderSignature = "X-AF-Webhook-Signature"
	HeaderTimestamp = "X-AF-Webhook-Timestamp"
	HeaderEvent     = "X-AF-Webhook-Event"
	HeaderDelivery  = "X-AF-Webhook-Delivery"
)

// NativeConfig tunes the drain loop + retry policy. Zero values fall back
// to the constants in DefaultNativeConfig.
type NativeConfig struct {
	// SigningSecret signs the request body (HMAC-SHA256). Empty -> the
	// POST is sent unsigned (still carries event + timestamp headers).
	SigningSecret string
	// MaxAttempts caps redelivery. After this many failed attempts the row
	// rests at status='failed' and is no longer claimed.
	MaxAttempts int
	// BackoffBase is the first retry delay; each subsequent retry doubles
	// it up to BackoffMax.
	BackoffBase time.Duration
	BackoffMax  time.Duration
	// PollInterval is how often the drain loop sweeps for due rows when no
	// enqueue nudge arrives.
	PollInterval time.Duration
	// BatchSize is the max rows claimed per sweep.
	BatchSize int
	// Timeout bounds a single delivery POST.
	Timeout time.Duration
	// AllowPrivateNetworks re-permits loopback + RFC-1918 destinations in
	// the SSRF guard. Off in production; on for dev/PoC where a subscriber
	// legitimately runs on localhost. Cloud-metadata (link-local) stays
	// blocked regardless.
	AllowPrivateNetworks bool
}

// DefaultNativeConfig returns the production defaults.
func DefaultNativeConfig() NativeConfig {
	return NativeConfig{
		MaxAttempts:  5,
		BackoffBase:  5 * time.Second,
		BackoffMax:   5 * time.Minute,
		PollInterval: 2 * time.Second,
		BatchSize:    20,
		Timeout:      15 * time.Second,
	}
}

func (c NativeConfig) withDefaults() NativeConfig {
	d := DefaultNativeConfig()
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = d.MaxAttempts
	}
	if c.BackoffBase <= 0 {
		c.BackoffBase = d.BackoffBase
	}
	if c.BackoffMax <= 0 {
		c.BackoffMax = d.BackoffMax
	}
	if c.PollInterval <= 0 {
		c.PollInterval = d.PollInterval
	}
	if c.BatchSize <= 0 {
		c.BatchSize = d.BatchSize
	}
	if c.Timeout <= 0 {
		c.Timeout = d.Timeout
	}
	return c
}

// NativeOutbound delivers outbound webhooks in-process using the
// suite_webhook_deliveries outbox as its durable queue.
type NativeOutbound struct {
	store  *DeliveryStore
	client *http.Client
	cfg    NativeConfig
	log    *slog.Logger

	// nudge wakes the drain loop immediately after an Enqueue so a fresh
	// delivery doesn't wait a full PollInterval. Buffered(1); sends are
	// best-effort (a full channel already means "work pending").
	nudge chan struct{}

	startOnce sync.Once
}

// NewNativeOutbound builds the in-process delivery service. store may be
// nil (Configured() then reports false and Enqueue returns ErrNotConfigured),
// mirroring the tolerant construction the rest of the package uses. The
// HTTP client is SSRF-guarded (safehttp) because destinations are
// caller-supplied external URLs.
func NewNativeOutbound(store *DeliveryStore, cfg NativeConfig, log *slog.Logger) *NativeOutbound {
	if log == nil {
		log = slog.Default()
	}
	cfg = cfg.withDefaults()
	opts := safehttp.Options{Timeout: cfg.Timeout}
	if cfg.AllowPrivateNetworks {
		opts.AllowCIDRs = safehttp.LoopbackAndPrivateCIDRs()
	}
	return &NativeOutbound{
		store:  store,
		client: safehttp.New(opts),
		cfg:    cfg,
		log:    log,
		nudge:  make(chan struct{}, 1),
	}
}

// Configured reports whether the outbound path can accept a send. Native
// delivery is available whenever the outbox DB is wired — /send only 503s
// when there's no database.
func (n *NativeOutbound) Configured() bool {
	return n != nil && n.store.HasPool()
}

// Enqueue writes a queued outbound row to the outbox and nudges the drain
// loop. The row is delivered asynchronously by the worker; the returned
// Delivery reflects its initial 'queued' state.
func (n *NativeOutbound) Enqueue(ctx context.Context, in SendInput) (*Delivery, error) {
	if !n.Configured() {
		return nil, ErrNotConfigured
	}
	if err := validateSendInput(&in); err != nil {
		return nil, err
	}
	var tenantPtr *string
	if t := strings.TrimSpace(in.TenantID); t != "" {
		tenantPtr = &t
	}
	d, err := n.store.Insert(ctx, InsertParams{
		TenantID:    tenantPtr,
		EndpointID:  in.EndpointID,
		Direction:   DirectionOutbound,
		Destination: in.URL,
		EventType:   in.EventType,
		Status:      StatusQueued,
		Headers:     in.Headers,
		Body:        in.Body,
		ScheduledAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	n.wake()
	return d, nil
}

// Retry re-queues a delivery (zeroes attempts, clears the error) and nudges
// the loop. The row is picked up on the next sweep.
func (n *NativeOutbound) Retry(ctx context.Context, deliveryID string) (*Delivery, error) {
	if !n.Configured() {
		return nil, ErrNotConfigured
	}
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return nil, fmt.Errorf("%w: id required", ErrInvalidInput)
	}
	if err := n.store.ResetForRetry(ctx, deliveryID); err != nil {
		return nil, err
	}
	n.wake()
	return n.store.Get(ctx, deliveryID)
}

// List / Get read straight from the outbox — the dashboard's outbound view
// is simply the outbound rows of suite_webhook_deliveries.
func (n *NativeOutbound) List(ctx context.Context, f ListFilters) (*ListResult, error) {
	if n == nil || !n.Configured() {
		return &ListResult{Deliveries: []Delivery{}}, nil
	}
	if f.Direction == DirectionInbound {
		// A native outbound service never owns inbound rows.
		return &ListResult{Deliveries: []Delivery{}}, nil
	}
	f.Direction = DirectionOutbound
	return n.store.List(ctx, f)
}

func (n *NativeOutbound) Get(ctx context.Context, deliveryID string) (*Delivery, error) {
	if n == nil || !n.Configured() {
		return nil, ErrNotFound
	}
	d, err := n.store.Get(ctx, deliveryID)
	if err != nil {
		return nil, err
	}
	if d.Direction != DirectionOutbound {
		// Let the facade fall through to the inbound store.
		return nil, ErrNotFound
	}
	return d, nil
}

// Start launches the drain loop. It runs until ctx is cancelled. Safe to
// call once; subsequent calls are no-ops. A nil/unconfigured service does
// nothing (so tests and DB-less runtimes stay quiet).
func (n *NativeOutbound) Start(ctx context.Context) {
	if n == nil || !n.Configured() {
		return
	}
	n.startOnce.Do(func() {
		go n.drainLoop(ctx)
	})
}

func (n *NativeOutbound) wake() {
	if n == nil {
		return
	}
	select {
	case n.nudge <- struct{}{}:
	default:
	}
}

// drainLoop sweeps the outbox for due rows on a ticker (or immediately on a
// nudge) and delivers each claimed batch.
func (n *NativeOutbound) drainLoop(ctx context.Context) {
	ticker := time.NewTicker(n.cfg.PollInterval)
	defer ticker.Stop()
	n.log.Info("webhooks: native outbound delivery worker started",
		"poll_interval", n.cfg.PollInterval.String(),
		"max_attempts", n.cfg.MaxAttempts)
	for {
		// Drain fully on each wake so a burst doesn't wait one tick per row.
		for {
			claimed, err := n.store.ClaimOutboundDue(ctx, n.cfg.BatchSize, n.cfg.MaxAttempts)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				n.log.Warn("webhooks: claim due deliveries failed", "error", err)
				break
			}
			if len(claimed) == 0 {
				break
			}
			for i := range claimed {
				n.deliverOne(ctx, &claimed[i])
			}
			if len(claimed) < n.cfg.BatchSize {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-n.nudge:
		}
	}
}

// deliverOne performs a single delivery attempt for an already-claimed
// ('delivering') row and records the terminal-or-retry state.
func (n *NativeOutbound) deliverOne(ctx context.Context, d *Delivery) {
	attempt := d.Attempts + 1
	status, respStatus, respMS, deliverErr := n.attempt(ctx, d)

	upd := UpdateParams{Attempts: &attempt}
	if respStatus != 0 {
		upd.ResponseStatus = &respStatus
	}
	if respMS >= 0 {
		ms := respMS
		upd.ResponseMS = &ms
	}
	if status == StatusSucceeded {
		upd.Status = StatusSucceeded
		now := time.Now().UTC()
		upd.DeliveredAt = &now
	} else {
		upd.Status = StatusFailed
		msg := "delivery failed"
		if deliverErr != nil {
			msg = deliverErr.Error()
		}
		upd.LastError = &msg
		// Schedule the next retry unless we've exhausted attempts; an
		// exhausted row keeps status='failed' but is never re-claimed
		// (ClaimOutboundDue filters attempts < MaxAttempts).
		if attempt < n.cfg.MaxAttempts {
			next := time.Now().UTC().Add(n.backoff(attempt))
			upd.ScheduledAt = &next
		}
	}
	if err := n.store.UpdateStatus(ctx, d.ID, upd); err != nil && ctx.Err() == nil {
		n.log.Warn("webhooks: record delivery result failed",
			"delivery_id", d.ID, "error", err)
	}
}

// attempt POSTs the delivery body to its destination and classifies the
// result. Returns (status, responseStatusCode, latencyMs, err). A 2xx is a
// success; anything else (non-2xx or transport/SSRF error) is a failure.
func (n *NativeOutbound) attempt(ctx context.Context, d *Delivery) (Status, int, int, error) {
	postCtx, cancel := context.WithTimeout(ctx, n.cfg.Timeout)
	defer cancel()

	body := d.Body
	if body == nil {
		body = []byte("null")
	}
	req, err := http.NewRequestWithContext(postCtx, http.MethodPost, d.Destination, bytes.NewReader(body))
	if err != nil {
		return StatusFailed, 0, -1, fmt.Errorf("%w: build request: %v", ErrTransport, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderEvent, d.EventType)
	req.Header.Set(HeaderDelivery, d.ID)
	ts := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	req.Header.Set(HeaderTimestamp, ts)
	if n.cfg.SigningSecret != "" {
		req.Header.Set(HeaderSignature, "sha256="+signBody(n.cfg.SigningSecret, body))
	}
	// Caller-supplied headers last so a delivery can override defaults.
	for k, v := range d.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := n.client.Do(req)
	latencyMS := int(time.Since(start).Milliseconds())
	if err != nil {
		if safehttp.IsBlocked(err) {
			return StatusFailed, 0, latencyMS, fmt.Errorf("%w: destination blocked (SSRF guard): %v", ErrTransport, err)
		}
		return StatusFailed, 0, latencyMS, fmt.Errorf("%w: %v", ErrTransport, err)
	}
	defer resp.Body.Close()
	// Drain a bounded prefix so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return StatusSucceeded, resp.StatusCode, latencyMS, nil
	}
	return StatusFailed, resp.StatusCode, latencyMS,
		fmt.Errorf("%w: status %d", ErrUpstreamFailure, resp.StatusCode)
}

// backoff returns the delay before the given attempt's retry: base * 2^(n-1)
// capped at BackoffMax.
func (n *NativeOutbound) backoff(attempt int) time.Duration {
	d := n.cfg.BackoffBase
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= n.cfg.BackoffMax {
			return n.cfg.BackoffMax
		}
	}
	if d > n.cfg.BackoffMax {
		d = n.cfg.BackoffMax
	}
	return d
}

// signBody returns the lowercase hex HMAC-SHA256 of body under secret.
func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// SignBody is the exported form used by the emit fan-out to sign each
// delivery with its subscription's secret. Returns the lowercase hex
// HMAC-SHA256 — pair it with the "sha256=" prefix in the signature header.
func SignBody(secret string, body []byte) string {
	return signBody(secret, body)
}

// validateSendInput enforces the bare minimum: a parseable absolute URL
// with http/https scheme and a non-empty event type. The HTTP layer
// already validates the JSON shape; this is the last line.
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
