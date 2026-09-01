// SPDX-License-Identifier: Apache-2.0

// reconcile_state.go — the pure decision layer for subscription lifecycle.
//
// Stripe is the source of truth for subscription status; this file maps a
// remote subscription snapshot onto the local entitlement + status the
// runtime should enforce. It is deliberately free of I/O so every drift
// case (trial, active, dunning, terminal failure) is unit-testable with a
// fabricated payload set, and so the webhook handler and the reconciliation
// cron share ONE definition of "what state should this tenant be in".
//
// Entitlement rule
//
//	active / trialing → full entitlements of the subscribed plan.
//	past_due          → GRACE: keep the subscribed plan's entitlements
//	                    (dunning window — Stripe is retrying payment).
//	terminal failure  → DEGRADE: drop to the default (free) plan.
//	                    (canceled, unpaid, incomplete_expired, paused,
//	                     incomplete — payment never established).
package billing

import "strings"

// DefaultPlan is the implicit free tier a tenant falls back to when it has
// no active paid subscription. Mirrors the "free" plan id used across the
// billing surface.
const DefaultPlan = "free"

// Stripe subscription status values we branch on. Kept as constants so the
// classifier and its tests reference one spelling.
const (
	StatusTrialing          = "trialing"
	StatusActive            = "active"
	StatusPastDue           = "past_due"
	StatusCanceled          = "canceled"
	StatusUnpaid            = "unpaid"
	StatusIncomplete        = "incomplete"
	StatusIncompleteExpired = "incomplete_expired"
	StatusPaused            = "paused"
)

// RemoteSubscription is the minimal snapshot of a Stripe subscription the
// reconciler needs. Populated from a webhook event OR from a Stripe API
// read during the drift-reconciliation cron.
type RemoteSubscription struct {
	// StripeCustomerID is the Stripe customer this subscription belongs to.
	StripeCustomerID string
	// Status is the Stripe subscription.status (see the constants above).
	Status string
	// Plan is the catalog plan id resolved from the subscription's price
	// ("" when the price maps to no catalog plan). Populated by the caller
	// after resolving StripePriceID against the plans catalog.
	Plan string
	// StripePriceID is the subscription's active price id (the reconcile
	// cron reads it from Stripe; the caller maps it to a catalog plan).
	StripePriceID string
	// CurrentPeriodEnd / TrialEnd are RFC3339 strings ("" when absent).
	CurrentPeriodEnd string
	TrialEnd         string
}

// StatusIsActive reports whether the status grants full entitlements
// (active or trialing).
func StatusIsActive(status string) bool {
	switch strings.TrimSpace(status) {
	case StatusActive, StatusTrialing:
		return true
	default:
		return false
	}
}

// StatusInGrace reports whether the status is a dunning/grace state where
// entitlements are retained while Stripe retries payment (past_due).
func StatusInGrace(status string) bool {
	return strings.TrimSpace(status) == StatusPastDue
}

// StatusIsTerminal reports whether the status means the subscription no
// longer entitles the tenant to a paid plan and entitlements must degrade
// to the default. This is the "terminal failure / no paid entitlement" set.
func StatusIsTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case StatusCanceled, StatusUnpaid, StatusIncompleteExpired, StatusPaused, StatusIncomplete:
		return true
	default:
		return false
	}
}

// EntitlementPlan returns the plan whose entitlements should currently apply
// given the subscription status. active/trialing/past_due keep the
// subscribed plan; any terminal or unrecognised status degrades to
// defaultPlan. An empty subscribedPlan also degrades (nothing to grant).
func EntitlementPlan(status, subscribedPlan, defaultPlan string) string {
	if defaultPlan == "" {
		defaultPlan = DefaultPlan
	}
	sp := strings.TrimSpace(subscribedPlan)
	if sp == "" {
		return defaultPlan
	}
	if StatusIsActive(status) || StatusInGrace(status) {
		return sp
	}
	return defaultPlan
}

// Reconciliation is the outcome of comparing a remote subscription snapshot
// with the local billing row.
type Reconciliation struct {
	// Plan is the entitlement plan the local row should carry.
	Plan string
	// Status is the subscription_status the local row should carry.
	Status string
	// Degraded describes the desired STATE: entitlements sit at the default
	// plan because of a terminal failure. Combine with Changed to detect the
	// transition into that state (Changed && Degraded), which is when the
	// caller should fire the downgrade notification / plan-applied hook.
	Degraded bool
	// Changed is true when the desired (plan,status) differs from current —
	// the caller only needs to write when Changed.
	Changed bool
}

// Reconcile computes the desired local state for a tenant from a remote
// subscription snapshot. It is the single decision used by both the webhook
// handler and the drift-reconciliation cron.
//
// current is the tenant's existing local billing row (its Plan +
// SubscriptionStatus are compared to detect drift). defaultPlan is the tier
// to degrade to on terminal failure ("" → "free").
func Reconcile(current Customer, remote RemoteSubscription, defaultPlan string) Reconciliation {
	if defaultPlan == "" {
		defaultPlan = DefaultPlan
	}
	desiredPlan := EntitlementPlan(remote.Status, remote.Plan, defaultPlan)
	desiredStatus := strings.TrimSpace(remote.Status)

	currentStatus := ""
	if current.SubscriptionStatus != nil {
		currentStatus = strings.TrimSpace(*current.SubscriptionStatus)
	}
	changed := desiredPlan != strings.TrimSpace(current.Plan) || desiredStatus != currentStatus

	return Reconciliation{
		Plan:     desiredPlan,
		Status:   desiredStatus,
		Degraded: StatusIsTerminal(remote.Status) && strings.TrimSpace(remote.Plan) != "" && desiredPlan == defaultPlan,
		Changed:  changed,
	}
}
