// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/traces"
)

type stubTracesStore struct {
	caps traces.Capabilities
	err  error
}

func (s stubTracesStore) Search(context.Context, traces.SearchFilter) (traces.SearchResult, error) {
	if s.err != nil {
		return traces.SearchResult{}, s.err
	}
	return traces.SearchResult{Traces: []traces.TraceSummary{{
		TraceID:       "abc",
		RootService:   "runtime",
		RootOperation: "GET /health",
		StartTime:     time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		Duration:      150 * time.Millisecond,
		SpanCount:     2,
		Status:        "ok",
	}}}, nil
}

func (s stubTracesStore) Get(_ context.Context, traceID string) (traces.Trace, error) {
	if s.err != nil {
		return traces.Trace{}, s.err
	}
	if traceID == "missing" {
		return traces.Trace{}, traces.ErrTraceNotFound
	}
	return traces.Trace{TraceID: traceID, Spans: []traces.Span{{
		SpanID:    "span-1",
		Service:   "runtime",
		Operation: "GET /health",
		StartTime: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		Duration:  150 * time.Millisecond,
		Status:    "ok",
	}}}, nil
}

func (s stubTracesStore) Capabilities() traces.Capabilities { return s.caps }

func TestAdminTracesSearchAndCapabilities(t *testing.T) {
	srv := New(config.Default(), testLogger(), Deps{TracesStore: stubTracesStore{caps: traces.Capabilities{SupportsTraceQL: true, NativeQueryLang: "traceql", MaxResultsPerQuery: 100}}})
	withOperator(srv, "owner") // routes are operator-gated (S1b)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/traces?service=runtime&limit=1", nil)
	rr := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out traceSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Traces) != 1 || out.Traces[0].TraceID != "abc" {
		t.Fatalf("out=%+v", out)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/traces/capabilities", nil)
	rr = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("caps status=%d body=%s", rr.Code, rr.Body.String())
	}
	var caps traces.Capabilities
	if err := json.Unmarshal(rr.Body.Bytes(), &caps); err != nil {
		t.Fatalf("decode caps: %v", err)
	}
	if !caps.SupportsTraceQL || caps.NativeQueryLang != "traceql" {
		t.Fatalf("caps=%+v", caps)
	}
}

func TestAdminTracesGetAndNotFound(t *testing.T) {
	srv := New(config.Default(), testLogger(), Deps{TracesStore: stubTracesStore{}})
	withOperator(srv, "owner")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/traces/abc", nil)
	rr := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out traceDetailResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.TraceID != "abc" || len(out.Spans) != 1 {
		t.Fatalf("out=%+v", out)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/traces/missing", nil)
	rr = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAdminTracesNoBackend(t *testing.T) {
	srv := New(config.Default(), testLogger(), Deps{TracesStore: stubTracesStore{err: traces.ErrNoBackend}})
	withOperator(srv, "owner")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/traces/abc", nil)
	rr := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	srv = New(config.Default(), testLogger(), Deps{TracesStore: stubTracesStore{err: errors.New("boom")}})
	withOperator(srv, "owner")
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/traces", nil)
	rr = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
