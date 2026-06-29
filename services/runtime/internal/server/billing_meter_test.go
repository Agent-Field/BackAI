// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/billing"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

// wiredBillingServer returns a server with a billing.Service that has no store
// (Meter is a safe no-op), which is enough to exercise the endpoint's
// validation + 204 path without a database.
func wiredBillingServer(t *testing.T) *Server {
	t.Helper()
	svc := billing.NewService(nil, nil, billing.NewMeterRegistry(), testLogger())
	return New(config.Default(), testLogger(), Deps{Billing: svc})
}

// Contract: a valid meter increment returns 204.
func TestBillingMeter_OK(t *testing.T) {
	srv := wiredBillingServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/meter",
		strings.NewReader(`{"name":"sandbox_seconds","qty":1.5,"tenant_id":"t1"}`))
	rr := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

// Contract: an empty meter name is a 400.
func TestBillingMeter_RejectsEmptyName(t *testing.T) {
	srv := wiredBillingServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/meter",
		strings.NewReader(`{"name":"","qty":1,"tenant_id":"t1"}`))
	rr := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// Contract: when billing isn't configured the endpoint reports 503, not a panic.
func TestBillingMeter_Unconfigured(t *testing.T) {
	srv := New(config.Default(), testLogger(), Deps{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/meter",
		strings.NewReader(`{"name":"x","qty":1,"tenant_id":"t1"}`))
	rr := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rr.Code, rr.Body.String())
	}
}
