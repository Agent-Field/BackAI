// SPDX-License-Identifier: Apache-2.0

// s1b_operator_gate_test.go — S1b: several read/control surfaces used to sit in
// publicPrefixes (bypassing the tenant resolver) and returned tenant-scoped
// data — or accepted an attacker-controlled ?tenant= filter — with no auth. A
// live probe confirmed unauthenticated GET /api/v1/secrets, /cost, /logs all
// returned 200. These are now operator-gated.
//
// Contract:
//   - Unauthenticated (no operator session) requests to the gated surfaces
//     return 401, never the data.
//   - An authenticated operator gets past the gate (handler behaviour resumes,
//     e.g. 503 when the backing store is unwired) — verified by the existing
//     secrets/runs tests which inject withOperator.
package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestS1bSurfacesRejectUnauthenticated(t *testing.T) {
	// method + path for each newly-gated surface. All must 401 without an
	// operator session, regardless of whether the backing store is wired.
	cases := []struct {
		method, path string
	}{
		{"GET", "/api/v1/secrets"},
		{"GET", "/api/v1/secrets/some_key"},
		{"POST", "/api/v1/secrets/some_key/reveal"},
		{"PUT", "/api/v1/secrets/some_key"},
		{"DELETE", "/api/v1/secrets/some_key"},
		{"POST", "/api/v1/secrets/some_key/rotate"},
		{"GET", "/api/v1/logs"},
		{"GET", "/api/v1/runs"},
		{"GET", "/api/v1/crons"},
		{"POST", "/api/v1/crons"},
		{"GET", "/api/v1/skills"},
		{"POST", "/api/v1/skills"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			s := newBareTestServer(t)
			withOperator(s, "") // simulate an unauthenticated caller
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s: expected 401 for unauthenticated caller, got %d body=%s",
					tc.method, tc.path, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestS1bSurfacesAllowOperator(t *testing.T) {
	// An authenticated operator must get PAST the gate. With no backing store
	// wired the handlers return 503/200/etc — anything but the 401/403 the gate
	// would emit. This proves the dashboard (which sends an operator session)
	// keeps working.
	cases := []string{
		"/api/v1/secrets",
		"/api/v1/logs",
		"/api/v1/runs",
		"/api/v1/crons",
		"/api/v1/skills",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			s := newBareTestServer(t)
			withOperator(s, "owner")
			req := httptest.NewRequest("GET", path, nil)
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, req)
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Fatalf("%s: operator was blocked by the gate (%d) — dashboard would break", path, rec.Code)
			}
		})
	}
}
