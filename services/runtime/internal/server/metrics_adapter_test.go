// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/metrics"
)

type stubMetricsStore struct {
	caps metrics.Capabilities
	err  error
}

func (s stubMetricsStore) Query(_ context.Context, promQL string, _ time.Time) ([]metrics.InstantSample, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []metrics.InstantSample{{
		Metric: map[string]string{"__name__": promQL},
		Value:  1,
		TS:     time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
	}}, nil
}

func (s stubMetricsStore) QueryRange(_ context.Context, promQL string, from, _ time.Time, _ time.Duration) ([]metrics.RangeSample, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []metrics.RangeSample{{
		Metric: map[string]string{"__name__": promQL},
		Values: []metrics.TimedValue{
			{TS: from, Value: 1},
			{TS: from.Add(time.Minute), Value: 2},
		},
	}}, nil
}

func (s stubMetricsStore) Capabilities() metrics.Capabilities { return s.caps }

func TestAdminMetricsQueryRangeAndCapabilities(t *testing.T) {
	srv := New(config.Default(), testLogger(), Deps{MetricsStore: stubMetricsStore{caps: metrics.Capabilities{SupportsInstantQuery: true, SupportsRangeQuery: true, NativeQueryLang: "promql"}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/metrics/query?promql=up%7B%7D", nil)
	rr := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("query status=%d body=%s", rr.Code, rr.Body.String())
	}
	var instant metricsInstantResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &instant); err != nil {
		t.Fatalf("decode query: %v", err)
	}
	if instant.Total != 1 || instant.Samples[0].Metric["__name__"] != "up{}" {
		t.Fatalf("instant=%+v", instant)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/metrics/range?promql=up%7B%7D&from=2026-06-16T12:00:00Z&to=2026-06-16T13:00:00Z&step=1m", nil)
	rr = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("range status=%d body=%s", rr.Code, rr.Body.String())
	}
	var rng metricsRangeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &rng); err != nil {
		t.Fatalf("decode range: %v", err)
	}
	if rng.Total != 1 || len(rng.Series[0].Values) != 2 {
		t.Fatalf("range=%+v", rng)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/metrics/capabilities", nil)
	rr = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("cap status=%d body=%s", rr.Code, rr.Body.String())
	}
	var caps metrics.Capabilities
	if err := json.Unmarshal(rr.Body.Bytes(), &caps); err != nil {
		t.Fatalf("decode caps: %v", err)
	}
	if caps.NativeQueryLang != "promql" {
		t.Fatalf("caps=%+v", caps)
	}
}

func TestAdminMetricsErrorsAndOpenAPI(t *testing.T) {
	srv := New(config.Default(), testLogger(), Deps{MetricsStore: stubMetricsStore{err: metrics.ErrNoBackend}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/metrics/query?promql=up%7B%7D", nil)
	rr := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	srv = New(config.Default(), testLogger(), Deps{MetricsStore: stubMetricsStore{err: errors.New("boom")}})
	req = httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rr = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("openapi status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "/api/v1/admin/metrics/query") ||
		!strings.Contains(rr.Body.String(), "/api/v1/admin/metrics/range") ||
		!strings.Contains(rr.Body.String(), "/api/v1/admin/metrics/capabilities") {
		t.Fatalf("openapi missing metrics paths: %s", rr.Body.String())
	}
}
