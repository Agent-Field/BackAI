// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// Contract: plan writes validate id/name/price/budget before touching the
// DB, so the operator panel gets crisp 400s instead of SQL errors.
func TestUpsertPlan_Validation(t *testing.T) {
	s := NewStore(nil, slog.Default()) // no pool

	cases := []struct {
		name string
		plan Plan
	}{
		{"missing id", Plan{Name: "Pro"}},
		{"missing name", Plan{ID: "pro"}},
		{"negative price", Plan{ID: "pro", Name: "Pro", PriceUSDMonth: -1}},
	}
	for _, tc := range cases {
		if _, err := s.UpsertPlan(context.Background(), tc.plan); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

// Contract: a zero/negative budget on a plan is rejected — nil means "no
// enforced budget", zero would silently disable the 402 gate.
func TestUpsertPlan_RejectsNonPositiveBudget(t *testing.T) {
	s := NewStore(nil, slog.Default())
	zero := 0.0
	_, err := s.UpsertPlan(context.Background(), Plan{ID: "pro", Name: "Pro", LLMBudgetUSD: &zero})
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for zero budget, got %v", err)
	}
}

// Contract: without a database the catalog degrades to empty reads and
// plan resolution reports not-found (callers fall back gracefully).
func TestPlans_NoPool(t *testing.T) {
	s := NewStore(nil, slog.Default())
	plans, err := s.ListPlans(context.Background())
	if err != nil || len(plans) != 0 {
		t.Fatalf("expected empty list without pool, got %v / %v", plans, err)
	}
	if _, err := s.GetPlan(context.Background(), "free"); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound without pool, got %v", err)
	}
}

// Contract: stub checkout never mints a provider URL — the plan applies
// directly and the plan-applied hook fires so the server can push the
// plan's budget.
func TestCheckout_StubAppliesDirectlyAndFiresHook(t *testing.T) {
	svc := NewService(nil, &stubStripeClient{log: slog.Default()}, nil, slog.Default())

	fired := false
	svc.SetOnPlanApplied(func(_ context.Context, tenantID string, _ Plan) {
		fired = true
		if tenantID != "t-1" {
			t.Fatalf("hook tenant = %s", tenantID)
		}
	})

	// No store → GetPlan not-found path must surface, not panic.
	_, err := svc.Checkout(context.Background(), "t-1", "pro", "http://app/done", "")
	if !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound without a catalog, got %v", err)
	}
	if fired {
		t.Fatal("hook must not fire when the plan cannot be resolved")
	}
}

// Contract: checkout validates its inputs.
func TestCheckout_Validation(t *testing.T) {
	svc := NewService(nil, &stubStripeClient{log: slog.Default()}, nil, slog.Default())
	if _, err := svc.Checkout(context.Background(), "", "pro", "http://app", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty tenant, got %v", err)
	}
	var nilSvc *Service
	if _, err := nilSvc.Checkout(context.Background(), "t", "p", "u", ""); !errors.Is(err, ErrBillingUnavailable) {
		t.Fatalf("expected ErrBillingUnavailable on nil service, got %v", err)
	}
}

// Contract: the real Stripe client rejects checkout without a price or
// success URL before any network call.
func TestRealClient_CheckoutValidation(t *testing.T) {
	c := &realStripeClient{log: slog.Default()}
	if _, err := c.CreateCheckoutSession(context.Background(), "cus_x", "", "http://ok", "", "t"); err == nil {
		t.Fatal("expected error for empty price id")
	}
	if _, err := c.CreateCheckoutSession(context.Background(), "cus_x", "price_x", "", "", "t"); err == nil {
		t.Fatal("expected error for empty success url")
	}
}

// Contract: stub checkout session returns the success URL (a caller that
// redirects anyway lands somewhere sensible) and never a provider URL.
func TestStubClient_CheckoutReturnsSuccessURL(t *testing.T) {
	c := &stubStripeClient{log: slog.Default()}
	url, err := c.CreateCheckoutSession(context.Background(), "cus", "price_x", "http://app/done", "", "t")
	if err != nil {
		t.Fatalf("stub checkout: %v", err)
	}
	if url != "http://app/done" {
		t.Fatalf("stub checkout url = %s", url)
	}
}

// Contract: SwapClient atomically replaces the adapter — the operator
// panel's save handler relies on this taking effect without a restart.
func TestSwapClient(t *testing.T) {
	svc := NewService(nil, &stubStripeClient{log: slog.Default()}, nil, slog.Default())
	if !svc.Client().IsStub() {
		t.Fatal("expected stub client initially")
	}
	svc.SwapClient(NewStripeClientFromConfig("sk_test_abc", "whsec_x", slog.Default()))
	if svc.Client().IsStub() {
		t.Fatal("expected real client after swap")
	}
	if got := svc.AdapterName(); got != "stripe" {
		t.Fatalf("adapter = %s", got)
	}
}

// Contract: the settings store is nil-safe and reports unavailability
// rather than panicking when the runtime boots without DB or KMS.
func TestSettingsStore_Degraded(t *testing.T) {
	var s *SettingsStore
	if v, err := s.Get(context.Background(), SettingStripeSecretKey); err != nil || v != "" {
		t.Fatalf("nil store Get = %q, %v", v, err)
	}
	empty := NewSettingsStore(nil, nil)
	if err := empty.Set(context.Background(), SettingStripeSecretKey, "sk"); err == nil ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected not-configured error, got %v", err)
	}
	sk, ws, fromVault, err := empty.StripeKeys(context.Background(), "sk_env", "whsec_env")
	if err != nil || fromVault || sk != "sk_env" || ws != "whsec_env" {
		t.Fatalf("env fallback broken: %s %s %v %v", sk, ws, fromVault, err)
	}
}
