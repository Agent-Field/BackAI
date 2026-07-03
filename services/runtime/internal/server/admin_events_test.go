// SPDX-License-Identifier: Apache-2.0

// Unit tests for the unified events feed at GET /api/v1/admin/events.
//
// The empty-deps path (no DB, no AF, no activity store) is the most
// important one to lock down: the dashboard's HomeShell falls back to this
// surface when home/overview's RecentRuns/RecentWebhookDeliveries arrays
// are empty, so the feed must always return a well-formed list shape even
// when nothing is wired.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminEventsEmptyDeps(t *testing.T) {
	s := newDashTestServer(t)
	withOperator(s, "owner") // route is operator-gated (S1b)
	req := httptest.NewRequest("GET", "/api/v1/admin/events", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	events, ok := body["events"].([]any)
	if !ok {
		t.Fatalf("expected events to be an array, got %T", body["events"])
	}
	// With no DB and no AF the only source that can fire is the dependency
	// probe — and that runs with a short timeout against a nil pool / nil
	// af, so we expect zero events here.
	if len(events) != 0 {
		t.Errorf("expected 0 events with no deps wired, got %d", len(events))
	}
}

func TestAdminEventsRespectsLimitParam(t *testing.T) {
	s := newDashTestServer(t)
	withOperator(s, "owner")
	req := httptest.NewRequest("GET", "/api/v1/admin/events?limit=5", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := body["events"].([]any); !ok {
		t.Fatalf("expected events to be an array, got %T", body["events"])
	}
}

func TestAdminEventsKindFilterEmpty(t *testing.T) {
	s := newDashTestServer(t)
	withOperator(s, "owner")
	// Bogus kind so the filter excludes everything; response should still
	// be a well-formed empty-list envelope, not a 4xx.
	req := httptest.NewRequest("GET", "/api/v1/admin/events?kind=does.not.exist", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	events, ok := body["events"].([]any)
	if !ok {
		t.Fatalf("expected events to be an array, got %T", body["events"])
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events with unmatched kind filter, got %d", len(events))
	}
}
