// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	stripe "github.com/stripe/stripe-go/v82"
)

// Contract item 7: the active Stripe mode is derivable from the key prefix
// (no API call), so operators can see live↔test at a glance.
func TestStripeKeyMode(t *testing.T) {
	cases := map[string]string{
		"sk_live_abc123":  "live",
		"rk_live_abc123":  "live",
		"sk_test_abc123":  "test",
		"rk_test_abc123":  "test",
		"  sk_test_pad  ": "test",
		"sk_weird_abc":    "",
		"":                "",
		"garbage":         "",
	}
	for key, want := range cases {
		if got := StripeKeyMode(key); got != want {
			t.Errorf("StripeKeyMode(%q) = %q, want %q", key, got, want)
		}
	}
}

// Contract item 6: a Stripe "resource_missing" error (the live↔test price
// symptom) is classified so checkout can surface ErrPriceStale.
func TestIsStripeResourceMissing(t *testing.T) {
	if !isStripeResourceMissing(&stripe.Error{Code: stripe.ErrorCodeResourceMissing}) {
		t.Error("resource_missing stripe error should classify as missing")
	}
	if isStripeResourceMissing(&stripe.Error{Code: stripe.ErrorCodeRateLimit}) {
		t.Error("a rate-limit error must not classify as resource_missing")
	}
	if isStripeResourceMissing(errors.New("plain error")) {
		t.Error("a non-Stripe error must not classify as resource_missing")
	}
	if isStripeResourceMissing(nil) {
		t.Error("nil must not classify as resource_missing")
	}
}

// Contract: only paid plans (price > 0) carry a Stripe price and get
// reconciled; free plans are skipped.
func TestPaidPlans_SkipsFree(t *testing.T) {
	plans := []Plan{
		{ID: "free", PriceUSDMonth: 0},
		{ID: "pro", PriceUSDMonth: 29},
		{ID: "protest", PriceUSDMonth: 19},
	}
	got := paidPlans(plans)
	if len(got) != 2 {
		t.Fatalf("paidPlans returned %d plans, want 2: %+v", len(got), got)
	}
	for _, p := range got {
		if p.ID == "free" {
			t.Errorf("free plan should never be reconciled")
		}
	}
}

// Contract item 4 (and safety): reconcile is a no-op without a store and in
// stub mode — it must never touch Stripe or panic.
func TestReconcilePlanPrices_NoopPaths(t *testing.T) {
	// No store at all.
	svc := NewService(nil, &stubStripeClient{log: slog.Default()}, nil, nil)
	if got, err := svc.ReconcilePlanPrices(context.Background()); err != nil || got != nil {
		t.Fatalf("nil-store reconcile = (%v, %v), want (nil, nil)", got, err)
	}

	// Store present but stub client → still a no-op (stub checkout applies
	// plans directly and ignores prices).
	stub := NewService(NewStore(nil, slog.Default()), &stubStripeClient{log: slog.Default()}, nil, slog.Default())
	if got, err := stub.ReconcilePlanPrices(context.Background()); err != nil || got != nil {
		t.Fatalf("stub reconcile = (%v, %v), want (nil, nil)", got, err)
	}
}
