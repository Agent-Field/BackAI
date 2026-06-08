// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGDPRExportReturns503WhenDatabaseMissing(t *testing.T) {
	s := newBareTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/admin/users/00000000-0000-0000-0000-000000000000/export", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "GDPR_NOT_CONFIGURED")
}

func TestGDPREraseReturns503WhenDatabaseMissing(t *testing.T) {
	s := newBareTestServer(t)
	req := httptest.NewRequest("POST", "/api/v1/admin/users/00000000-0000-0000-0000-000000000000/erase", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "GDPR_NOT_CONFIGURED")
}
