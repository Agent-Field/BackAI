// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/traces"
)

func TestStore_SearchGetCapabilities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":             "trace-echo",
				"slot":             "traces",
				"protocol_version": "v1",
				"capabilities": map[string]any{
					"supports_traceql":      true,
					"supports_tag_search":   true,
					"native_query_lang":     "traceql",
					"max_results_per_query": 100,
				},
			})
		case "/v1/traces/search":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"traces": []map[string]any{{
					"trace_id":       "abc",
					"root_service":   "runtime",
					"root_operation": "root",
					"start_time":     time.Unix(10, 0).UTC(),
					"duration":       1000000,
					"span_count":     1,
					"status":         "ok",
				}},
			})
		case "/v1/traces/abc":
			_ = json.NewEncoder(w).Encode(traces.Trace{TraceID: "abc", Spans: []traces.Span{{SpanID: "1"}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	store, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !store.Capabilities().SupportsTraceQL || store.AdapterName() != "trace-echo" {
		t.Fatalf("unexpected caps/name: %+v %s", store.Capabilities(), store.AdapterName())
	}
	result, err := store.Search(context.Background(), traces.SearchFilter{Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Traces) != 1 || result.Traces[0].TraceID != "abc" {
		t.Fatalf("unexpected result: %+v", result)
	}
	trace, err := store.Get(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if trace.TraceID != "abc" || len(trace.Spans) != 1 {
		t.Fatalf("unexpected trace: %+v", trace)
	}
}

func TestStore_GetNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":             "trace-echo",
				"slot":             "traces",
				"protocol_version": "v1",
				"capabilities":     map[string]any{},
			})
		case "/v1/traces/missing":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": 404,
				"code":   "trace_not_found",
				"detail": "trace not found",
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
	_, err = store.Get(context.Background(), "missing")
	if !errors.Is(err, traces.ErrTraceNotFound) {
		t.Fatalf("expected ErrTraceNotFound, got %v", err)
	}
}
