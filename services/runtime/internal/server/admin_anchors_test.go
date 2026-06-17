// SPDX-License-Identifier: Apache-2.0

// Unit tests for the unified top-bar anchors endpoint at
// GET /api/v1/admin/anchors. The endpoint is polled on every page so its
// empty-deps shape must always be well-formed; the dashboard reads
// `inbox_has_critical` to drive the badge colour, so its presence and
// boolean type are both load-bearing.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminAnchorsEmptyDeps(t *testing.T) {
	s := newDashTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/admin/anchors", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// Empty-deps: no approvals store, no probes can fail -> zero pending,
	// no critical flag, healthy. Validate every field is present with the
	// right zero-value type so AdminAnchorsSchema (zod) parses it.
	if _, ok := body["inbox_pending"].(float64); !ok {
		t.Fatalf("expected inbox_pending number, got %T", body["inbox_pending"])
	}
	critical, ok := body["inbox_has_critical"].(bool)
	if !ok {
		t.Fatalf("expected inbox_has_critical bool, got %T", body["inbox_has_critical"])
	}
	if critical {
		t.Errorf("expected inbox_has_critical=false with no probes wired, got true")
	}
	if _, ok := body["cost_today_usd"].(float64); !ok {
		t.Fatalf("expected cost_today_usd number, got %T", body["cost_today_usd"])
	}
	if health, ok := body["health"].(string); !ok {
		t.Fatalf("expected health string, got %T", body["health"])
	} else if health != "healthy" {
		t.Errorf("expected healthy (no failing probes), got %q", health)
	}
}
