// SPDX-License-Identifier: Apache-2.0

// Tests for the pull-worker REST surface. The full lease -> heartbeat ->
// complete flow runs against a live Postgres (the rendezvous store is
// DB-backed) and is exercised by scripts/worker-conformance downstream;
// these tests cover the wiring + the no-manager envelope, which need no DB.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Every worker route must be registered and, with no jobs.Manager wired,
// return a structured 503 (not a 404 from an unregistered path, and not a
// collision with GET /api/v1/jobs/{id}).
func TestWorkerRoutesRegisteredReturn503WithoutManager(t *testing.T) {
	s := newDashTestServer(t)
	for _, path := range []string{
		"/api/v1/jobs/worker/lease",
		"/api/v1/jobs/worker/heartbeat",
		"/api/v1/jobs/worker/complete",
		"/api/v1/jobs/worker/fail",
		"/api/v1/jobs/worker/logs",
	} {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: expected 503, got %d body=%s", path, rec.Code, rec.Body.String())
		}
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: invalid JSON envelope: %v", path, err)
		}
		if body.Error.Code != "JOBS_NOT_CONFIGURED" {
			t.Errorf("%s: error.code = %q, want JOBS_NOT_CONFIGURED", path, body.Error.Code)
		}
	}
}

// A worker route must not shadow the existing single-job GET route.
func TestWorkerRoutesDoNotShadowGetJob(t *testing.T) {
	s := newDashTestServer(t)
	// GET /api/v1/jobs/{id} with a nil manager returns 503 from handleGetJob;
	// if the worker routes had swallowed it we'd see a different code/path.
	req := httptest.NewRequest("GET", "/api/v1/jobs/123", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /api/v1/jobs/123: expected 503, got %d", rec.Code)
	}
}
