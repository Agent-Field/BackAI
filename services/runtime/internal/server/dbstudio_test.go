// Package server — tests for the DB studio REST endpoints.
//
// These cover the empty-Studio path (no DB at boot) and confirm 503
// envelopes look right. Real DB-backed paths are covered by the
// integration tests in services/runtime/internal/dbstudio gated by
// `-tags=integration`.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListDBTablesNoStudioReturns503(t *testing.T) {
	s := newDashTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/db/tables", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
	// Envelope shape.
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	envelope, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error envelope: %v", body)
	}
	if envelope["code"] != "DB_STUDIO_NOT_CONFIGURED" {
		t.Errorf("error.code = %v, want DB_STUDIO_NOT_CONFIGURED", envelope["code"])
	}
}

func TestGetDBTableNoStudioReturns503(t *testing.T) {
	s := newDashTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/db/tables/public/users", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestDBRowsNoStudioReturns503(t *testing.T) {
	s := newDashTestServer(t)
	req := httptest.NewRequest("GET",
		"/api/v1/db/rows?schema=public&table=users&limit=10&offset=0", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestDBSQLNoStudioReturns503(t *testing.T) {
	s := newDashTestServer(t)
	req := httptest.NewRequest("POST", "/api/v1/db/sql",
		strings.NewReader(`{"statement":"select 1","read_only":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDBSQLInvalidJSONReturns503WithoutStudio(t *testing.T) {
	// With no studio wired, the handler should still 503 (early
	// short-circuit) and never reach the JSON parser.
	s := newDashTestServer(t)
	req := httptest.NewRequest("POST", "/api/v1/db/sql", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 (no studio), got %d", rec.Code)
	}
}
