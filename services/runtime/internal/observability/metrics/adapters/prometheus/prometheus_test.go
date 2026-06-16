// SPDX-License-Identifier: Apache-2.0

package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/metrics"
)

func TestPrometheusQueryDecodesVectorAndProbesCadvisor(t *testing.T) {
	var gotReady, gotContainer bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case readinessPath:
			gotReady = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
		case statusConfigPath:
			writeJSON(t, w, map[string]any{"status": "success", "data": map[string]any{"yaml": "storage.tsdb.retention.time: 15d\n"}})
		case instantQueryPath:
			if r.URL.Query().Get("query") == cadvisorCPUQuery {
				gotContainer = true
			}
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": map[string]any{
					"resultType": "vector",
					"result": []any{map[string]any{
						"metric": map[string]string{"__name__": "up", "job": "prometheus"},
						"value":  []any{1718548800.5, "1"},
					}},
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	store, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	caps := store.Capabilities()
	if !gotReady || !gotContainer || !caps.SupportsContainer || caps.RetentionHours != 24*15 {
		t.Fatalf("gotReady=%v gotContainer=%v caps=%+v", gotReady, gotContainer, caps)
	}
	samples, err := store.Query(context.Background(), "up{}", time.Time{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(samples) != 1 || samples[0].Metric["job"] != "prometheus" || samples[0].Value != 1 {
		t.Fatalf("samples=%+v", samples)
	}
}

func TestPrometheusRangeDecodesMatrixAndFormatsStep(t *testing.T) {
	var gotStep string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case readinessPath:
			w.WriteHeader(http.StatusOK)
		case statusConfigPath:
			writeJSON(t, w, map[string]any{"status": "success", "data": map[string]any{"yaml": ""}})
		case instantQueryPath:
			writeJSON(t, w, map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": []any{}}})
		case rangeQueryPath:
			gotStep = r.URL.Query().Get("step")
			writeJSON(t, w, map[string]any{
				"status": "success",
				"data": map[string]any{
					"resultType": "matrix",
					"result": []any{map[string]any{
						"metric": map[string]string{"__name__": "up"},
						"values": []any{[]any{1718548800.0, "1"}, []any{1718548860.0, "2"}},
					}},
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	store, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	from := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	series, err := store.QueryRange(context.Background(), "up{}", from, from.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if gotStep != "60s" {
		t.Fatalf("step=%q want 60s", gotStep)
	}
	if len(series) != 1 || len(series[0].Values) != 2 || series[0].Values[1].Value != 2 {
		t.Fatalf("series=%+v", series)
	}
}

func TestPrometheusRejectsUnsupportedShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case readinessPath:
			w.WriteHeader(http.StatusOK)
		case statusConfigPath:
			writeJSON(t, w, map[string]any{"status": "success", "data": map[string]any{"yaml": ""}})
		case instantQueryPath:
			writeJSON(t, w, map[string]any{"status": "success", "data": map[string]any{"resultType": "scalar", "result": []any{}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	store, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := store.Query(context.Background(), "scalar(1)", time.Time{}); err == nil || !errors.Is(err, metrics.ErrUnsupportedShape) {
		t.Fatalf("err=%v want ErrUnsupportedShape", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode: %v", err)
	}
}
