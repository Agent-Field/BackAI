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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	t.Setenv("AF_STACK_BILLING_ADAPTER", "stripe")
	t.Setenv("STRIPE_SECRET_KEY", "")
	c := billing.NewClientFromEnv(nil)
	if !c.IsStub() {
		t.Fatal("expected stub client when STRIPE_SECRET_KEY is empty")
	}

	id1, err := c.CreateCustomer(context.Background(), "tenant-1", "user@example.com")
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}
	id2, err := c.CreateCustomer(context.Background(), "tenant-1", "user@example.com")
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
	t.Setenv("AF_STACK_BILLING_ADAPTER", "stripe")
	t.Setenv("STRIPE_SECRET_KEY", "")
	c := billing.NewClientFromEnv(nil)
	link, err := c.CreatePortalLink(context.Background(), "cus_stub_x", "https://app.example.com/back")
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
	t.Setenv("AF_STACK_BILLING_ADAPTER", "stripe")
	t.Setenv("STRIPE_SECRET_KEY", "")
	c := billing.NewClientFromEnv(nil)
	cust, err := c.GetCustomer(context.Background(), "cus_stub_x")
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

func TestNewClientFromEnv_LagoStub(t *testing.T) {
	t.Setenv("AF_STACK_BILLING_ADAPTER", "lago")
	t.Setenv("LAGO_API_URL", "")
	t.Setenv("LAGO_API_KEY", "")
	c := billing.NewClientFromEnv(nil)
	if c == nil {
		t.Fatal("expected lago stub client")
	}
	if c.AdapterName() != "lago" {
		t.Fatalf("AdapterName = %q, want lago", c.AdapterName())
	}
	if !c.IsStub() {
		t.Fatal("expected lago stub when env is incomplete")
	}
	id, err := c.CreateCustomer(context.Background(), "tenant-abc", "")
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}
	if id != "lago_stub_tenant-abc" {
		t.Fatalf("id = %q, want tenant-derived lago stub id", id)
	}
	link, err := c.CreatePortalLink(context.Background(), id, "https://app.example.com")
	if err != nil {
		t.Fatalf("CreatePortalLink: %v", err)
	}
	if !strings.Contains(link.URL, "lago-portal-stub") {
		t.Fatalf("link URL = %q, want lago stub URL", link.URL)
	}
}

func TestLagoClient_CreateCustomerAndPortal(t *testing.T) {
	var sawAuth bool
	var createdExternalID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer lago-key" {
			sawAuth = true
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/customers":
			var body struct {
				Customer struct {
					ExternalID string `json:"external_id"`
					Email      string `json:"email"`
				} `json:"customer"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode lago create body: %v", err)
			}
			createdExternalID = body.Customer.ExternalID
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"customer":{"external_id":"tenant-123","email":"user@example.com"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/customers/tenant-123/portal_url":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"customer":{"portal_url":"https://lago.example.com/customer/portal"}}`))
		default:
			t.Fatalf("unexpected Lago request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("AF_STACK_BILLING_ADAPTER", "lago")
	t.Setenv("LAGO_API_URL", server.URL)
	t.Setenv("LAGO_API_KEY", "lago-key")
	c := billing.NewClientFromEnv(nil)
	if c.AdapterName() != "lago" || c.IsStub() {
		t.Fatalf("expected real lago adapter, got name=%q stub=%v", c.AdapterName(), c.IsStub())
	}
	id, err := c.CreateCustomer(context.Background(), "tenant-123", "user@example.com")
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}
	if id != "tenant-123" || createdExternalID != "tenant-123" {
		t.Fatalf("external id mismatch: id=%q created=%q", id, createdExternalID)
	}
	link, err := c.CreatePortalLink(context.Background(), id, "")
	if err != nil {
		t.Fatalf("CreatePortalLink: %v", err)
	}
	if link.URL != "https://lago.example.com/customer/portal" {
		t.Fatalf("portal URL = %q", link.URL)
	}
	if !sawAuth {
		t.Fatal("Lago adapter did not send bearer auth")
	}
}

// ─── Service degraded modes ──────────────────────────────────────────────

func TestService_NilStoreReturnsEmpty(t *testing.T) {
	t.Setenv("AF_STACK_BILLING_ADAPTER", "stripe")
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
	t.Setenv("AF_STACK_BILLING_ADAPTER", "stripe")
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
