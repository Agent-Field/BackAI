// SPDX-License-Identifier: Apache-2.0

package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetupWithNoEndpointSucceeds(t *testing.T) {
	tel, err := Setup(context.Background(), Config{ServiceName: "test"})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if tel == nil || tel.TracerProvider == nil || tel.Registry == nil {
		t.Fatal("expected non-nil Telemetry, TracerProvider, Registry")
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestMetricsHandlerServesPrometheusFormat(t *testing.T) {
	tel, err := Setup(context.Background(), Config{ServiceName: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer tel.Shutdown(context.Background())

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	tel.MetricsHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); len(got) < 100 {
		t.Errorf("expected non-trivial metrics body, got %q", got)
	}
}

func TestNilTelemetryMetricsHandlerIs503(t *testing.T) {
	var tel *Telemetry
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	tel.MetricsHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestNilTelemetryShutdownIsNoOp(t *testing.T) {
	var tel *Telemetry
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Errorf("expected nil shutdown error for nil telemetry, got %v", err)
	}
}