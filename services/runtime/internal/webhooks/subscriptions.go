// SPDX-License-Identifier: Apache-2.0

// subscriptions.go — tenant-owned outbound webhook subscriptions.
//
// A Subscription is a URL a tenant registers to RECEIVE its own domain
// events. It backs the tenant-facing send/receive model: the tenant
// registers subscribers (POST /api/v1/webhooks/subscriptions) and emits
// events (POST /api/v1/webhooks/emit) which the runtime fans out — as
// native, signed deliveries — ONLY to that tenant's active subscribers.
//
// Isolation. suite_webhook_subscriptions is RLS-scoped by tenant_id and
// every store method runs on the request context (whose connection is
// bound to app.tenant_id via db.PrepareConn). So a tenant can only see,
// delete, or fan out to its own subscriptions — this is scoped delivery,
// not the open outbound relay the operator-gated /send guards against.
package webhooks

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Subscription is the persisted + wire shape. Secret is only populated on
// Create (returned once) and on ActiveForEvent (needed to sign); List
// leaves it empty so it never leaks to the dashboard.
type Subscription struct {
	ID        string   `json:"id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Secret    string   `json:"secret,omitempty"`
	IsActive  bool     `json:"is_active"`
	CreatedAt string   `json:"created_at"`
}

// SubscriptionStore wraps a pgxpool for suite_webhook_subscriptions.
type SubscriptionStore struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewSubscriptionStore returns nil when pool is nil so callers can compose
// unconditionally and degrade to "not configured" at the boundary.
func NewSubscriptionStore(pool *pgxpool.Pool, log *slog.Logger) *SubscriptionStore {
	if pool == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &SubscriptionStore{pool: pool, log: log}
}

// HasPool reports whether the store is wired to a database.
func (s *SubscriptionStore) HasPool() bool {
	return s != nil && s.pool != nil
}

// Create registers a subscriber for the caller's tenant (bound via ctx →
// RLS). It generates a signing secret and returns the row with that secret
// (the only time it's surfaced). events may be empty (subscribe to all).
func (s *SubscriptionStore) Create(ctx context.Context, rawURL string, events []string) (*Subscription, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	rawURL = strings.TrimSpace(rawURL)
	if err := validateSubscriptionURL(rawURL); err != nil {
		return nil, err
	}
	events = normaliseEvents(events)
	secret := generateSubscriptionSecret()

	const q = `
        insert into suite_webhook_subscriptions (tenant_id, url, events, secret)
        values (nullif(current_setting('app.tenant_id', true), '')::uuid, $1, $2, $3)
        returning id, url, events, secret, is_active, created_at::text
    `
	row := s.pool.QueryRow(ctx, q, rawURL, events, secret)
	sub, err := scanSubscription(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: subscription already exists", ErrInvalidInput)
		}
		return nil, fmt.Errorf("webhooks: create subscription: %w", err)
	}
	return sub, nil
}

// List returns the caller tenant's subscriptions (RLS-scoped), newest
// first. The signing secret is redacted.
func (s *SubscriptionStore) List(ctx context.Context) ([]Subscription, error) {
	if !s.HasPool() {
		return []Subscription{}, nil
	}
	const q = `
        select id, url, events, ''::text as secret, is_active, created_at::text
        from suite_webhook_subscriptions
        order by created_at desc
    `
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("webhooks: list subscriptions: %w", err)
	}
	defer rows.Close()
	out := make([]Subscription, 0)
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, fmt.Errorf("webhooks: scan subscription: %w", err)
		}
		out = append(out, *sub)
	}
	return out, rows.Err()
}

// Delete removes a subscription by id, scoped to the caller's tenant via
// RLS (a cross-tenant id simply affects zero rows → ErrNotFound).
func (s *SubscriptionStore) Delete(ctx context.Context, id string) error {
	if !s.HasPool() {
		return ErrNotConfigured
	}
	tag, err := s.pool.Exec(ctx, `delete from suite_webhook_subscriptions where id = $1`, id)
	if err != nil {
		return fmt.Errorf("webhooks: delete subscription: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ActiveForEvent returns the caller tenant's active subscriptions that want
// eventType (an empty events array matches every event). Secrets ARE
// included so the emit path can sign each delivery. RLS scopes the result
// to the bound tenant.
func (s *SubscriptionStore) ActiveForEvent(ctx context.Context, eventType string) ([]Subscription, error) {
	if !s.HasPool() {
		return []Subscription{}, nil
	}
	const q = `
        select id, url, events, secret, is_active, created_at::text
        from suite_webhook_subscriptions
        where is_active
          and (cardinality(events) = 0 or $1 = any(events))
        order by created_at desc
    `
	rows, err := s.pool.Query(ctx, q, eventType)
	if err != nil {
		return nil, fmt.Errorf("webhooks: subscriptions for event: %w", err)
	}
	defer rows.Close()
	out := make([]Subscription, 0)
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, fmt.Errorf("webhooks: scan subscription: %w", err)
		}
		out = append(out, *sub)
	}
	return out, rows.Err()
}

func scanSubscription(r rowScanner) (*Subscription, error) {
	var sub Subscription
	if err := r.Scan(&sub.ID, &sub.URL, &sub.Events, &sub.Secret, &sub.IsActive, &sub.CreatedAt); err != nil {
		return nil, err
	}
	if sub.Events == nil {
		sub.Events = []string{}
	}
	return &sub, nil
}

// validateSubscriptionURL enforces an absolute http(s) URL. SSRF (private
// destinations) is enforced at delivery time by safehttp; this is the
// cheap up-front shape check.
func validateSubscriptionURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("%w: url is required", ErrInvalidInput)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: url must be absolute http(s)", ErrInvalidInput)
	}
	switch u.Scheme {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("%w: url scheme must be http or https", ErrInvalidInput)
	}
}

// normaliseEvents trims, drops empties, and dedups event names.
func normaliseEvents(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, e := range in {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

// generateSubscriptionSecret returns a "whsec_"-prefixed base32 secret.
func generateSubscriptionSecret() string {
	var b [30]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read never fails on supported platforms; degrade to a
		// non-empty (but weak) marker rather than an empty secret.
		return "whsec_unavailable"
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
	return "whsec_" + strings.ToLower(enc)
}
