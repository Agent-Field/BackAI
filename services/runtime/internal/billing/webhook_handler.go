// SPDX-License-Identifier: Apache-2.0

// webhook_handler.go — Stripe webhook ingest.
//
// Stripe pushes lifecycle events to /webhooks/in/stripe; this file
// verifies the signature (via the official stripe-go library), decodes
// the event, and dispatches to the right write path on the Service.
//
// Phase 10.4 handles two events explicitly:
//
//   - customer.subscription.updated — upsert the local Customer row with
//     the new plan / period_end / status.
//   - invoice.payment_succeeded     — record the period close so the
//     dashboard knows the previous period is settled.
//
// Every other event type is acknowledged (200) but otherwise ignored.
// Stripe retries on 5xx; we never want to bounce a verified event for a
// shape we don't care about — that just generates retry storms.
//
// Coordination with Phase 10.2: the inbound webhook framework owns the
// route at /webhooks/in/{slug}. We register /webhooks/in/stripe as a
// DIRECT handler in server.go (NOT via the inbound table) because the
// signature verification + dispatch is Stripe-specific. If Phase 10.2
// claims the slug "stripe" in suite_webhook_endpoints, this handler
// wins (mux matches before the catch-all inbound handler — see the
// comment block in server.go that registers the route).

package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
)

// HandleStripeWebhook verifies the signature and applies the event to
// the local store.
//
// body is the raw HTTP request body (verbatim — DO NOT re-marshal it,
// the signature is computed over the exact bytes).
// sigHeader is the value of the Stripe-Signature HTTP header.
//
// Returns nil on success (including no-op events). Returns
// ErrSignatureInvalid when verification fails — the REST layer maps
// that to 400 so Stripe doesn't retry. Returns other errors when the DB
// write fails — those map to 500 so Stripe retries.
func (s *Service) HandleStripeWebhook(ctx context.Context, body []byte, sigHeader string) error {
	if s == nil {
		return errors.New("billing: service not configured")
	}
	if s.client == nil {
		return errors.New("billing: stripe client not configured")
	}
	if s.client.AdapterName() != "stripe" {
		return fmt.Errorf("%w: stripe webhook received while billing adapter is %s", ErrInvalidInput, s.client.AdapterName())
	}

	// Stub mode: there's no webhook secret + the stub client's
	// VerifyWebhook always fails. We accept the event JSON without
	// verification so dev / CI can exercise the dispatch path.
	var ev *stripe.Event
	if s.client.IsStub() {
		var raw stripe.Event
		if err := json.Unmarshal(body, &raw); err != nil {
			return fmt.Errorf("billing: stub webhook decode: %w", err)
		}
		ev = &raw
		s.log.Debug("billing: stub-mode webhook accepted without signature check",
			"type", ev.Type)
	} else {
		v, err := s.client.VerifyWebhook(body, sigHeader)
		if err != nil {
			return err
		}
		ev = v
	}

	return s.dispatchEvent(ctx, ev)
}

// dispatchEvent routes a verified event to the right Service method.
// Unknown event types are no-ops (acknowledged 200).
func (s *Service) dispatchEvent(ctx context.Context, ev *stripe.Event) error {
	// A well-formed Stripe event always carries a data object, but a
	// hand-crafted / truncated payload (common in stub mode, where the
	// signature check is skipped) can arrive with no `data` key, leaving
	// ev.Data nil. The per-type handlers read ev.Data.Raw, so guard here
	// rather than nil-deref (which otel span.End turns into a double panic).
	if ev.Data == nil {
		s.log.Warn("billing: webhook event has no data object", "type", ev.Type, "event_id", ev.ID)
		return nil
	}
	switch ev.Type {
	case "customer.subscription.updated", "customer.subscription.created":
		return s.handleSubscriptionUpdated(ctx, ev)
	case "customer.subscription.deleted":
		return s.handleSubscriptionDeleted(ctx, ev)
	case "invoice.payment_succeeded":
		return s.handleInvoicePaymentSucceeded(ctx, ev)
	default:
		s.log.Debug("billing: webhook event ignored", "type", ev.Type)
		return nil
	}
}

// subscriptionEvent is the minimal subset of stripe.Subscription we
// pull out of the event JSON. We don't decode the whole struct because
// Stripe's subscription shape is large and we only care about a few
// fields.
type subscriptionEvent struct {
	Customer         string `json:"customer"`
	Status           string `json:"status"`
	CurrentPeriodEnd int64  `json:"current_period_end"`
	TrialEnd         int64  `json:"trial_end"`
	Items            struct {
		Data []struct {
			Plan struct {
				Nickname string `json:"nickname"`
				Product  string `json:"product"`
			} `json:"plan"`
			Price struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
	Metadata map[string]string `json:"metadata"`
}

func (s *Service) handleSubscriptionUpdated(ctx context.Context, ev *stripe.Event) error {
	var sub subscriptionEvent
	if err := json.Unmarshal(ev.Data.Raw, &sub); err != nil {
		return fmt.Errorf("billing: decode subscription event: %w", err)
	}
	tenantID := tenantIDFromMetadata(sub.Metadata)
	if tenantID == "" {
		// We rely on the subscription metadata carrying tenant_id
		// (set when the runtime first provisions the customer). Without
		// it we can't link the event to a local row.
		s.log.Warn("billing: subscription event missing tenant_id metadata",
			"customer", sub.Customer, "event_id", ev.ID)
		return nil
	}
	if s.store == nil || !s.store.HasPool() {
		return nil
	}
	stripeID := sub.Customer
	// Resolve the catalog plan from the subscription's Stripe Price id —
	// that binding is what the operator configured on the plans page. The
	// price nickname is only a fallback for uncatalogued prices.
	plan := ""
	var catalogPlan *Plan
	if len(sub.Items.Data) > 0 {
		if priceID := strings.TrimSpace(sub.Items.Data[0].Price.ID); priceID != "" {
			if p, perr := s.store.PlanByStripePrice(ctx, priceID); perr == nil {
				catalogPlan = &p
				plan = p.ID
			}
		}
		if plan == "" {
			plan = strings.TrimSpace(sub.Items.Data[0].Plan.Nickname)
		}
	}
	if plan == "" {
		plan = "free"
	}
	c := Customer{
		TenantID:         tenantID,
		StripeCustomerID: &stripeID,
		Plan:             plan,
	}
	if sub.Status != "" {
		st := sub.Status
		c.SubscriptionStatus = &st
	}
	if sub.CurrentPeriodEnd > 0 {
		t := time.Unix(sub.CurrentPeriodEnd, 0).UTC().Format(time.RFC3339Nano)
		c.CurrentPeriodEnd = &t
	}
	if sub.TrialEnd > 0 {
		t := time.Unix(sub.TrialEnd, 0).UTC().Format(time.RFC3339Nano)
		c.TrialEndsAt = &t
	}
	if _, err := s.store.UpsertCustomer(ctx, c); err != nil {
		return fmt.Errorf("billing: upsert from subscription: %w", err)
	}
	// Close the loop: applying a plan is more than recording it — the
	// server-side hook pushes the plan's enforced LLM budget etc.
	if catalogPlan != nil {
		s.firePlanApplied(ctx, tenantID, *catalogPlan)
	}
	return nil
}

func (s *Service) handleSubscriptionDeleted(ctx context.Context, ev *stripe.Event) error {
	var sub subscriptionEvent
	if err := json.Unmarshal(ev.Data.Raw, &sub); err != nil {
		return fmt.Errorf("billing: decode subscription event: %w", err)
	}
	tenantID := tenantIDFromMetadata(sub.Metadata)
	if tenantID == "" || s.store == nil || !s.store.HasPool() {
		return nil
	}
	stripeID := sub.Customer
	canceled := "canceled"
	c := Customer{
		TenantID:           tenantID,
		StripeCustomerID:   &stripeID,
		Plan:               "free",
		SubscriptionStatus: &canceled,
	}
	if _, err := s.store.UpsertCustomer(ctx, c); err != nil {
		return fmt.Errorf("billing: upsert from subscription delete: %w", err)
	}
	return nil
}

// invoiceEvent is the minimal subset of stripe.Invoice we need.
type invoiceEvent struct {
	Customer string            `json:"customer"`
	Metadata map[string]string `json:"metadata"`
	// PeriodEnd is the end of the billing period this invoice covers
	// (seconds since unix epoch). We use it to update the local row's
	// current_period_end so the dashboard knows the close.
	PeriodEnd int64 `json:"period_end"`
}

func (s *Service) handleInvoicePaymentSucceeded(ctx context.Context, ev *stripe.Event) error {
	var inv invoiceEvent
	if err := json.Unmarshal(ev.Data.Raw, &inv); err != nil {
		return fmt.Errorf("billing: decode invoice event: %w", err)
	}
	tenantID := tenantIDFromMetadata(inv.Metadata)
	if tenantID == "" || s.store == nil || !s.store.HasPool() {
		return nil
	}
	if inv.PeriodEnd == 0 {
		return nil
	}
	stripeID := inv.Customer
	t := time.Unix(inv.PeriodEnd, 0).UTC().Format(time.RFC3339Nano)
	c := Customer{
		TenantID:         tenantID,
		StripeCustomerID: &stripeID,
		Plan:             "", // empty -> default applies on insert; on update plan stays put via the coalesce
		CurrentPeriodEnd: &t,
	}
	if _, err := s.store.UpsertCustomer(ctx, c); err != nil {
		return fmt.Errorf("billing: upsert from invoice: %w", err)
	}
	return nil
}

// tenantIDFromMetadata returns the tenant_id stored in Stripe metadata
// (set at customer-provisioning time). Returns "" when absent.
func tenantIDFromMetadata(md map[string]string) string {
	if md == nil {
		return ""
	}
	for _, k := range []string{"tenant_id", "af_tenant_id", "tenantId"} {
		if v, ok := md[k]; ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
