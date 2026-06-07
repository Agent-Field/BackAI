// SPDX-License-Identifier: Apache-2.0

// Unit tests for the billing package.
//
// These tests exercise the no-DB paths (Store==nil), the stub Stripe
// client, the period boundary math, and the service-level fallthroughs.
// DB-backed integration tests live behind the `integration` build tag
// and require a live Postgres + the 00011_billing.sql migration applied.

package billing_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/billing"
)

// ─── PeriodBoundary ──────────────────────────────────────────────────────

func TestPeriodBoundary_MonthFloorsToFirst(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 34, 56, 0, time.UTC)
	start, end := billing.PeriodBoundary(billing.BucketMonth, now)
	if start != time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("month start = %v, want 2026-06-01T00:00:00Z", start)
	}
	if end != time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("month end = %v, want 2026-07-01T00:00:00Z", end)
	}
}

func TestPeriodBoundary_DayFloorsToMidnight(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 34, 56, 0, time.UTC)
	start, end := billing.PeriodBoundary(billing.BucketDay, now)
	if start != time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC) {
		t.Errorf("day start = %v, want 2026-06-15T00:00:00Z", start)
	}
	if end != time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC) {
		t.Errorf("day end = %v, want 2026-06-16T00:00:00Z", end)
	}
}

func TestPeriodBoundary_UnknownBucketFallsBackToMonth(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 34, 56, 0, time.UTC)
	start, end := billing.PeriodBoundary(billing.Bucket("year"), now)
	if start != time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("fallback start = %v, want month-aligned", start)
	}
	if end != time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("fallback end = %v, want month-aligned", end)
	}
}

// ─── Stub Stripe client ──────────────────────────────────────────────────

func TestStubClient_CreateCustomerDeterministic(t *testing.T) {
	// No STRIPE_SECRET_KEY -> stub.
	t.Setenv("STRIPE_SECRET_KEY", "")
	c := billing.NewClientFromEnv(nil)
	if !c.IsStub() {
		t.Fatal("expected stub client when STRIPE_SECRET_KEY is empty")
	}

	id1, err := c.CreateCustomer("user@example.com")
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}
	id2, err := c.CreateCustomer("user@example.com")
	if err != nil {
		t.Fatalf("CreateCustomer (second call): %v", err)
	}
	if id1 != id2 {
		t.Errorf("stub ids differ for same email: %q vs %q", id1, id2)
	}
	if !strings.HasPrefix(id1, "cus_stub_") {
		t.Errorf("stub id = %q, want cus_stub_ prefix", id1)
	}
}

func TestStubClient_PortalLinkPointsAtExample(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "")
	c := billing.NewClientFromEnv(nil)
	link, err := c.CreatePortalLink("cus_stub_x", "https://app.example.com/back")
	if err != nil {
		t.Fatalf("CreatePortalLink: %v", err)
	}
	if !strings.HasPrefix(link.URL, "https://example.com/") {
		t.Errorf("stub URL = %q, want example.com prefix", link.URL)
	}
	if link.ExpiresAt == "" {
		t.Error("stub portal expires_at empty")
	}
}

func TestStubClient_GetCustomerReturnsFreeRow(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "")
	c := billing.NewClientFromEnv(nil)
	cust, err := c.GetCustomer("cus_stub_x")
	if err != nil {
		t.Fatalf("GetCustomer: %v", err)
	}
	if cust.Plan != "free" {
		t.Errorf("stub plan = %q, want free", cust.Plan)
	}
	if cust.StripeCustomerID == nil || *cust.StripeCustomerID != "cus_stub_x" {
		t.Errorf("stub stripe_customer_id mismatch: %+v", cust.StripeCustomerID)
	}
}

// ─── Service degraded modes ──────────────────────────────────────────────

func TestService_NilStoreReturnsEmpty(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "")
	svc := billing.NewService(nil, billing.NewClientFromEnv(nil), nil, nil)
	ctx := context.Background()

	customers, err := svc.ListCustomers(ctx)
	if err != nil {
		t.Errorf("ListCustomers (nil store): unexpected err %v", err)
	}
	if len(customers) != 0 {
		t.Errorf("ListCustomers (nil store) = %d rows, want 0", len(customers))
	}

	meters, err := svc.ListMeters(ctx, billing.MeterFilters{})
	if err != nil {
		t.Errorf("ListMeters (nil store): unexpected err %v", err)
	}
	if len(meters) != 0 {
		t.Errorf("ListMeters (nil store) = %d rows, want 0", len(meters))
	}

	// Get returns ErrNotFound on nil store so the REST handler synthesises.
	if _, err := svc.GetCustomer(ctx, "00000000-0000-0000-0000-000000000001"); !errors.Is(err, billing.ErrNotFound) {
		t.Errorf("GetCustomer (nil store) err = %v, want ErrNotFound", err)
	}
}

func TestService_NilStoreMeterIsNoop(t *testing.T) {
	svc := billing.NewService(nil, nil, nil, nil)
	if err := svc.Meter(context.Background(), "sandbox_seconds", 1.0, "tenant1"); err != nil {
		t.Errorf("Meter (nil store) err = %v, want nil", err)
	}
}

func TestService_HasBudgetPermissiveWithoutStore(t *testing.T) {
	svc := billing.NewService(nil, nil, nil, nil)
	ok, err := svc.HasBudget(context.Background(), "tenant1", 1000.0)
	if err != nil {
		t.Errorf("HasBudget err = %v, want nil", err)
	}
	if !ok {
		t.Errorf("HasBudget (nil store) = false, want permissive true")
	}
}

func TestService_PortalLinkWithoutClientFails(t *testing.T) {
	svc := billing.NewService(nil, nil, nil, nil)
	_, err := svc.PortalLink(context.Background(), "tenant1", "")
	if !errors.Is(err, billing.ErrStripeUnavailable) {
		t.Errorf("PortalLink (nil client) err = %v, want ErrStripeUnavailable", err)
	}
}

func TestService_PortalLinkWithStubProvisionsAndReturns(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "")
	svc := billing.NewService(nil, billing.NewClientFromEnv(nil), nil, nil)
	link, err := svc.PortalLink(context.Background(), "tenant-123", "https://app.example.com")
	if err != nil {
		t.Fatalf("PortalLink: %v", err)
	}
	if !strings.HasPrefix(link.URL, "https://example.com/") {
		t.Errorf("portal URL = %q, want example.com prefix", link.URL)
	}
}

// ─── MeterRegistry ────────────────────────────────────────────────────────

func TestMeterRegistry_PriceLookup(t *testing.T) {
	r := billing.NewMeterRegistry()
	if got := r.Price("sandbox_seconds"); got <= 0 {
		t.Errorf("sandbox_seconds price = %v, want > 0", got)
	}
	if got := r.Price("unknown_meter"); got != 0 {
		t.Errorf("unknown meter price = %v, want 0", got)
	}
	r.Register("custom_meter", 0.01)
	if got := r.Price("custom_meter"); got != 0.01 {
		t.Errorf("registered price = %v, want 0.01", got)
	}
}