// openapi_test.go — verifies the live /openapi.json endpoint produces a
// valid OpenAPI 3.1 spec at runtime.
//
// The lightweight assertions here mirror the dashboard's API explorer
// expectations: top-level keys, openapi version string, and a minimum
// path count so the spec is meaningfully populated rather than the
// initial empty stub.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

func TestOpenAPIEndpointReturnsValidThreeOneSpec(t *testing.T) {
	srv := New(config.Default(), slog.Default(), Deps{})
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var spec map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, key := range []string{"openapi", "info", "paths"} {
		if _, ok := spec[key]; !ok {
			t.Errorf("spec missing top-level key %q", key)
		}
	}
	if v, _ := spec["openapi"].(string); v != "3.1.0" {
		t.Errorf("openapi = %q, want 3.1.0", v)
	}
	paths, _ := spec["paths"].(map[string]any)
	if len(paths) < 10 {
		t.Errorf("paths = %d entries, want >= 10. Have: %v", len(paths), keysOf(paths))
	}
	// Spot-check: a few critical routes should be present.
	expected := []string{
		"/health",
		"/ready",
		"/openapi.json",
		"/api/v1/agents",
		"/api/v1/agents/{call}",
	}
	for _, p := range expected {
		if _, ok := paths[p]; !ok {
			t.Errorf("expected path %q not registered", p)
		}
	}
}

func TestOpenAPISpecHasInfoTitleAndVersion(t *testing.T) {
	srv := New(config.Default(), slog.Default(), Deps{})
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	var spec map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &spec)
	info, ok := spec["info"].(map[string]any)
	if !ok {
		t.Fatal("info missing or wrong type")
	}
	if info["title"] == "" {
		t.Errorf("info.title is empty")
	}
	if info["version"] == "" {
		t.Errorf("info.version is empty")
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
