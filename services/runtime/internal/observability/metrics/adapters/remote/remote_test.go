// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRemoteMetricsStore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			writeJSON(t, w, map[string]any{
				"name":             "metrics-echo",
				"version":          "1.0.0",
				"slot":             "metrics",
				"protocol_version": "v1",
				"capabilities": map[string]any{
					"supports_instant_query":     true,
					"supports_range_query":       true,
					"supports_container_metrics": false,
					"native_query_lang":          "promql",
					"max_series_per_query":       100,
				},
			})
		case "/v1/metrics/query":
			if r.Method != http.MethodPost {
				t.Fatalf("method=%s", r.Method)
			}
			var req queryRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode query request: %v", err)
			}
			if req.Query != "up{}" {
				t.Fatalf("query=%q", req.Query)
			}
			writeJSON(t, w, map[string]any{"samples": []any{map[string]any{
				"metric": map[string]string{"__name__": "up"},
				"value":  1,
				"ts":     "2026-06-16T12:00:00Z",
			}}})
		case "/v1/metrics/query_range":
			if r.Method != http.MethodPost {
				t.Fatalf("method=%s", r.Method)
			}
			var req rangeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode range request: %v", err)
			}
			if req.Query != "up{}" || req.Step != "1m0s" {
				t.Fatalf("request=%+v", req)
			}
			writeJSON(t, w, map[string]any{"series": []any{map[string]any{
				"metric": map[string]string{"__name__": "up"},
				"values": []any{map[string]any{"ts": "2026-06-16T12:00:00Z", "value": 1}},
			}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	store, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if caps := store.Capabilities(); !caps.SupportsInstantQuery || caps.NativeQueryLang != "promql" {
		t.Fatalf("caps=%+v", caps)
	}
	samples, err := store.Query(context.Background(), "up{}", time.Time{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(samples) != 1 || samples[0].Value != 1 {
		t.Fatalf("samples=%+v", samples)
	}
	series, err := store.QueryRange(context.Background(), "up{}", time.Now().Add(-time.Minute), time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if len(series) != 1 || len(series[0].Values) != 1 {
		t.Fatalf("series=%+v", series)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode: %v", err)
	}
}
