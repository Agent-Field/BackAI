// SPDX-License-Identifier: Apache-2.0

// Package billing wires an external billing provider + the runtime's per-tenant usage meter
// aggregation, behind a single Service that the REST handlers and SDKs
// consume.
//
// The package is structured in three layers:
//
//   - Store  — direct SQL against suite_billing_customers + suite_usage_meters.
//   - Client — provider adapter (Stripe or Lago) with deterministic stub
//     modes for dev.
//   - Service — composes Store + Client, gating budgets and producing the
//     wire shapes consumed by the dashboard.
//
// Contract notes
//
//	The wire schemas live in apps/dashboard/src/lib/api.ts under the
//	"Billing (Phase 10.4)" section. The Customer / UsageMeter / PortalLink
//	struct field tags here MUST mirror the zod schemas there — the
//	dashboard's safeParse will reject anything drifting.
//
//	Nullable wire fields use pointer types so json.Marshal emits a JSON
//	null rather than the zero value. The "free" plan is the implicit
//	default and is NOT stored in Stripe — a tenant on free never has a
//	stripe_customer_id.
package billing

import (
	"errors"
	"time"
)

// Sentinel errors. The REST layer maps each to a code + HTTP status.
var (
	// ErrNotFound is returned by Store.GetCustomer when no row exists
	// for a given tenant_id.
	ErrNotFound = errors.New("billing: not found")

	// ErrInvalidInput is returned when the caller's request fails
	// validation (empty tenant id, negative budget, etc.).
	ErrInvalidInput = errors.New("billing: invalid input")

	// ErrBillingUnavailable is returned when the active billing adapter
	// can't reach its upstream API. Distinct from configuration errors
	// so the REST layer can translate this to a 502.
	ErrBillingUnavailable = errors.New("billing: provider unavailable")

	// ErrStripeUnavailable is kept for old tests/callers; use
	// ErrBillingUnavailable in new code.
	ErrStripeUnavailable = ErrBillingUnavailable

	// ErrSignatureInvalid is returned by the webhook verifier when the
	// Stripe-Signature header doesn't match the body.
	ErrSignatureInvalid = errors.New("billing: invalid webhook signature")

	// ErrPriceStale is returned when a checkout references a Stripe Price
	// that doesn't exist under the currently-active key — typically after
	// an operator swaps keys (e.g. live→test), which orphans every Price
	// provisioned by the old key. The REST layer maps this to a 409 with
	// guidance to re-provision the plan.
	ErrPriceStale = errors.New("billing: plan price not found for the active Stripe key")
)

// Customer mirrors BillingCustomerSchema in apps/dashboard/src/lib/api.ts.
//
// StripeCustomerID is nullable because multi-tenancy lets a tenant exist
// before it's been provisioned in Stripe (e.g. trial tenants on the free
// plan). Plan is the active subscription plan ("free", "pro",
// "enterprise"); "free" is the implicit default and is NOT stored in
// Stripe. The remaining nullable fields only have meaning once a Stripe
// subscription exists.
type Customer struct {
	TenantID           string  `json:"tenant_id"`
	StripeCustomerID   *string `json:"stripe_customer_id"`
	Email              *string `json:"email"`
	Plan               string  `json:"plan"`
	TrialEndsAt        *string `json:"trial_ends_at"`
	CurrentPeriodEnd   *string `json:"current_period_end"`
	SubscriptionStatus *string `json:"subscription_status"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

// UsageMeter mirrors UsageMeterSchema. One row aggregates a (meter,
// tenant_id, period_start) bucket — quantity accumulates whatever native
// unit the meter measures, and cost_usd accumulates priced spend (null
// when the meter is purely informational).
type UsageMeter struct {
	Meter         string   `json:"meter"`
	TenantID      string   `json:"tenant_id"`
	PeriodStart   string   `json:"period_start"`
	PeriodEnd     string   `json:"period_end"`
	Quantity      float64  `json:"quantity"`
	CostUSD       *float64 `json:"cost_usd"`
	StripeMeterID *string  `json:"stripe_meter_id"`
	LastSyncedAt  *string  `json:"last_synced_at"`
}

// PortalLink mirrors PortalLinkSchema. URL is a Stripe Customer Portal
// session URL; in stub mode it points at a deterministic placeholder
// (example.com) so the dashboard can render the button.
type PortalLink struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

// Bucket is the aggregation granularity for the meters list endpoint.
// Mirrors the bucket query parameter in api.billing.meters().
type Bucket string

const (
	// BucketMonth is the default: one row per calendar month per
	// (meter, tenant). Period start is date_trunc('month', now()).
	BucketMonth Bucket = "month"

	// BucketDay is for the daily slice view used by spend charts.
	BucketDay Bucket = "day"
)

// PeriodBoundary returns the [start, end) window for the given bucket and
// "now". For month, it floors to the first day of the calendar month and
// rolls forward one month. For day, it floors to midnight UTC and rolls
// forward 24h. now is taken as UTC; callers that need local-tz buckets
// should pre-shift.
func PeriodBoundary(b Bucket, now time.Time) (start, end time.Time) {
	now = now.UTC()
	switch b {
	case BucketDay:
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 0, 1)
	default:
		// BucketMonth — also the safe fallback for unknown values.
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 1, 0)
	}
	return start, end
}
