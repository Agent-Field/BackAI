// SPDX-License-Identifier: Apache-2.0

package tempo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/traces"
)

func TestNew_ProbeVersionSelectsTraceQL(t *testing.T) {
	for _, tc := range []struct {
		name     string
		version  string
		traceQL  bool
		nativeQL string
	}{
		{name: "tempo 2", version: "2.7.1", traceQL: true, nativeQL: "traceql"},
		{name: "tempo 1", version: "1.5.0", traceQL: false},
		{name: "bad version", version: "not-semver", traceQL: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/status/version" {
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"version": tc.version})
			}))
			defer srv.Close()

			store, err := New(context.Background(), Config{BaseURL: srv.URL})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			caps := store.Capabilities()
			if caps.SupportsTraceQL != tc.traceQL || caps.NativeQueryLang != tc.nativeQL {
				t.Fatalf("caps = %+v", caps)
			}
			if !caps.SupportsTagSearch {
				t.Fatalf("tag search should always be supported")
			}
		})
	}
}

func TestVersionFromText(t *testing.T) {
	raw := "tempo, version v3.0.0 (branch: main, revision: abc)"
	if got := versionFromText(raw); got != "3.0.0" {
		t.Fatalf("versionFromText=%q", got)
	}
}

func TestSearch_TraceQLUsesAPISearchQ(t *testing.T) {
	var seen url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "2.4.0"})
		case "/api/search":
			seen = r.URL.Query()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"traces": []map[string]any{{
					"traceID":           "abc",
					"rootServiceName":   "runtime",
					"rootTraceName":     "POST /api",
					"startTimeUnixNano": time.Unix(100, 0).UnixNano(),
					"durationMs":        123,
					"spanCount":         2,
					"status":            "error",
				}},
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
	got, err := store.Search(context.Background(), traces.SearchFilter{
		Service:   "runtime",
		Operation: "POST /api",
		Status:    "error",
		Tag:       map[string]string{"http.method": "POST"},
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if seen.Get("q") == "" {
		t.Fatalf("TraceQL search should use q param, got %s", seen.Encode())
	}
	if seen.Get("tags") != "" {
		t.Fatalf("TraceQL search should not use tags param, got %s", seen.Encode())
	}
	if strings.Contains(seen.Encode(), "api%2Fv2%2Fsearch") {
		t.Fatalf("must not use /api/v2/search")
	}
	if len(got.Traces) != 1 || got.Traces[0].TraceID != "abc" || got.Traces[0].Status != "error" {
		t.Fatalf("unexpected search result: %+v", got)
	}
}

func TestSearch_LegacyUsesSingleTagsParamAndTenant(t *testing.T) {
	var seenTags string
	var seenTenant string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "1.5.0"})
		case "/api/search":
			seenTags = r.URL.Query().Get("tags")
			seenTenant = r.Header.Get("X-Scope-OrgID")
			_ = json.NewEncoder(w).Encode(map[string]any{"traces": []map[string]any{}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	store, err := New(context.Background(), Config{BaseURL: srv.URL, Tenant: "tenant-a"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = store.Search(context.Background(), traces.SearchFilter{
		Service: "runtime",
		Tag:     map[string]string{"http.method": "POST"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if seenTenant != "tenant-a" {
		t.Fatalf("tenant header = %q", seenTenant)
	}
	if !strings.Contains(seenTags, "service.name=runtime") || !strings.Contains(seenTags, "http.method=POST") {
		t.Fatalf("legacy tags param = %q", seenTags)
	}
}

func TestGet_DecodesScopeAndInstrumentationSpans(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "2.4.0"})
		case "/api/traces/abc":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"traceID":"abc",
				"batches":[{
					"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"runtime"}}]},
					"scopeSpans":[{"spans":[{
						"traceID":"abc",
						"spanID":"0000000000000001",
						"parentSpanID":"0000000000000000",
						"name":"root",
						"startTimeUnixNano":"1000000000",
						"endTimeUnixNano":"1500000000",
						"attributes":[{"key":"http.method","value":{"stringValue":"GET"}}],
						"events":[{"timeUnixNano":"1200000000","name":"mark","attributes":[{"key":"step","value":{"stringValue":"one"}}]}],
						"status":{"code":"STATUS_CODE_OK"}
					}]}],
					"instrumentationLibrarySpans":[{"spans":[{
						"traceID":"abc",
						"spanID":"0000000000000002",
						"parentSpanID":"0000000000000001",
						"name":"child",
						"startTimeUnixNano":"1600000000",
						"endTimeUnixNano":"1700000000",
						"status":{"code":"STATUS_CODE_ERROR"}
					}]}]
				}]
			}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	store, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	trace, err := store.Get(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(trace.Spans) != 2 {
		t.Fatalf("span count = %d", len(trace.Spans))
	}
	if trace.Spans[0].ParentSpanID != "" {
		t.Fatalf("root parent should normalize to empty, got %q", trace.Spans[0].ParentSpanID)
	}
	if trace.Spans[0].Service != "runtime" || trace.Spans[0].Status != "ok" || len(trace.Spans[0].Events) != 1 {
		t.Fatalf("root span = %+v", trace.Spans[0])
	}
	if trace.Spans[1].Status != "error" {
		t.Fatalf("child span = %+v", trace.Spans[1])
	}
}

func TestGet_NotFoundMapsTraceNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "2.4.0"})
		case "/api/traces/missing":
			http.NotFound(w, r)
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
