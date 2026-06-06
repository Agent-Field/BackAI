// Package server — tests for the dashboard read-only endpoints.
//
// These tests exercise the empty-DB / empty-deps path (no PG, no AF) and
// confirm responses still match the zod schemas in apps/dashboard/src/lib/api.ts:
// arrays are non-nil, nullable fields are emitted as JSON null, and field
// names are snake_case.
//
// Real DB-backed paths (run history with rows present, hourly bucketing) are
// covered by integration tests that spin up Postgres — out of scope for the
// unit suite which must run without external services.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

// newDashTestServer builds a Server with no DB and no AF wired up. The
// dashboard handlers should all degrade gracefully to empty payloads.
func newDashTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.Default()
	return New(cfg, slog.Default(), Deps{})
}

// ─── /api/v1/runs ─────────────────────────────────────────────────────────

func TestListRunsEmptyDB(t *testing.T) {
	s := newDashTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/runs", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// Required shape: {runs: [], total: 0, has_more: false}
	runs, ok := body["runs"].([]any)
	if !ok {
		t.Fatalf("expected runs to be an array, got %T", body["runs"])
	}
	if len(runs) != 0 {
		t.Errorf("expected empty runs, got %d", len(runs))
	}
	total, ok := body["total"].(float64)
	if !ok {
		t.Fatalf("expected total to be a number, got %T", body["total"])
	}
	if total != 0 {
		t.Errorf("expected total=0, got %v", total)
	}
	hasMore, ok := body["has_more"].(bool)
	if !ok {
		t.Fatalf("expected has_more to be a boolean, got %T", body["has_more"])
	}
	if hasMore {
		t.Errorf("expected has_more=false, got true")
	}
}

func TestListRunsAcceptsAllFilterParams(t *testing.T) {
	// Verify the handler runs cleanly when every filter and paging param
	// is supplied. With no DB, the body should still be an empty list.
	s := newDashTestServer(t)
	req := httptest.NewRequest("GET",
		"/api/v1/runs?agent=sample.echo&tenant=00000000-0000-0000-0000-000000000000&status=succeeded&limit=10&offset=0",
		nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestParsePagingClampsBounds(t *testing.T) {
	cases := []struct {
		name               string
		limitIn, offsetIn  string
		wantLimit, wantOff int
	}{
		{"defaults when empty", "", "", 50, 0},
		{"non-positive limit ignored; negative offset ignored", "0", "-5", 50, 0},
		{"limit clamped to 200", "500", "100", 200, 100},
		{"non-numeric falls back to defaults", "abc", "xyz", 50, 0},
		{"valid values pass through", "75", "25", 75, 25},
		{"limit=1 is the floor", "1", "0", 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotLimit, gotOff := parsePaging(c.limitIn, c.offsetIn)
			if gotLimit != c.wantLimit || gotOff != c.wantOff {
				t.Errorf("parsePaging(%q,%q) = (%d,%d) want (%d,%d)",
					c.limitIn, c.offsetIn, gotLimit, gotOff, c.wantLimit, c.wantOff)
			}
		})
	}
}

func TestAgentFromEndpoint(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/api/v1/execute/sample.echo", "sample.echo"},
		{"/api/v1/execute/async/sample.echo", "sample.echo"},
		{"/api/v1/execute/ns.fn_v2", "ns.fn_v2"},
		{"/other/path", "/other/path"},
		{"", ""},
	}
	for _, c := range cases {
		got := agentFromEndpoint(c.in)
		if got != c.want {
			t.Errorf("agentFromEndpoint(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

// ─── /api/v1/home/overview ────────────────────────────────────────────────

func TestHomeOverviewEmptyDeps(t *testing.T) {
	s := newDashTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/home/overview", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// KPI fields are present and numeric.
	for _, k := range []string{"requests_per_minute", "error_rate", "cost_today_usd", "queue_depth"} {
		if _, ok := body[k].(float64); !ok {
			t.Errorf("expected %s to be a number, got %T", k, body[k])
		}
	}

	// All four sparklines exist, are arrays of length 24, all zeros.
	for _, k := range []string{
		"request_sparkline", "error_sparkline", "cost_sparkline", "queue_sparkline",
	} {
		arr, ok := body[k].([]any)
		if !ok {
			t.Errorf("expected %s to be an array, got %T", k, body[k])
			continue
		}
		if len(arr) != 24 {
			t.Errorf("expected %s len=24, got %d", k, len(arr))
		}
	}

	// Required array fields are present and non-nil (zod requires arrays).
	for _, k := range []string{"recent_runs", "recent_webhook_deliveries", "alerts"} {
		if _, ok := body[k].([]any); !ok {
			t.Errorf("expected %s to be an array, got %T (value=%v)", k, body[k], body[k])
		}
	}
}

func TestHomeOverviewAlertsWhenAFUnreachable(t *testing.T) {
	// Point the AF client at a closed port so .Health() fails fast.
	cfg := config.Default()
	srv := New(cfg, slog.Default(), Deps{
		AF: agentfield.New(agentfield.Config{URL: "http://127.0.0.1:1"}),
	})
	req := httptest.NewRequest("GET", "/api/v1/home/overview", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	alerts, _ := body["alerts"].([]any)
	if len(alerts) == 0 {
		t.Fatalf("expected at least one alert when AF unreachable, got none")
	}
	found := false
	for _, a := range alerts {
		m, _ := a.(map[string]any)
		if m["id"] == "agentfield-unreachable" {
			found = true
			if m["severity"] != "critical" {
				t.Errorf("expected critical severity, got %v", m["severity"])
			}
			break
		}
	}
	if !found {
		t.Errorf("expected agentfield-unreachable alert id, got alerts=%v", alerts)
	}
}

// ─── /api/v1/cost ─────────────────────────────────────────────────────────

func TestCostSummaryStub(t *testing.T) {
	s := newDashTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/cost?from=2026-01-01&to=2026-01-31", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Use json.RawMessage to inspect the "budget_usd" null literal directly —
	// json.Unmarshal into map[string]any maps JSON null to a nil interface,
	// so we have to look at the raw bytes for that one field.
	raw := rec.Body.String()
	if !strings.Contains(raw, `"budget_usd":null`) {
		t.Errorf("expected budget_usd to be JSON null, got body=%s", raw)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Numeric stubs default to 0.
	for _, k := range []string{"period_total_usd", "previous_total_usd", "forecast_usd"} {
		v, ok := body[k].(float64)
		if !ok {
			t.Errorf("expected %s to be a number, got %T", k, body[k])
			continue
		}
		if v != 0 {
			t.Errorf("expected %s=0 in stub, got %v", k, v)
		}
	}

	// budget_usd present (as null), confirmed it lives in the map.
	if _, ok := body["budget_usd"]; !ok {
		t.Errorf("expected budget_usd field to be present (even if null)")
	}

	// Breakdown arrays exist and are empty.
	for _, k := range []string{"by_day", "by_model", "by_agent", "by_tenant"} {
		arr, ok := body[k].([]any)
		if !ok {
			t.Errorf("expected %s to be an array, got %T", k, body[k])
			continue
		}
		if len(arr) != 0 {
			t.Errorf("expected %s to be empty in stub, got %d items", k, len(arr))
		}
	}
}

// ─── /api/v1/modules ──────────────────────────────────────────────────────

func TestModulesStateDefaultV1Catalogue(t *testing.T) {
	s := newDashTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/modules", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	mods, ok := body["modules"].([]any)
	if !ok {
		t.Fatalf("expected modules to be an array, got %T", body["modules"])
	}
	if len(mods) != len(v1ModuleCatalogue) {
		t.Errorf("expected %d modules, got %d", len(v1ModuleCatalogue), len(mods))
	}

	// Build a map of returned modules for easier assertions.
	byID := map[string]map[string]any{}
	for _, m := range mods {
		mm, _ := m.(map[string]any)
		id, _ := mm["id"].(string)
		byID[id] = mm
	}

	// multi-tenancy and sandbox default OFF.
	if mt, ok := byID["multi-tenancy"]; !ok {
		t.Errorf("missing multi-tenancy entry")
	} else if mt["enabled"] != false {
		t.Errorf("expected multi-tenancy enabled=false, got %v", mt["enabled"])
	}
	if sb, ok := byID["sandbox"]; !ok {
		t.Errorf("missing sandbox entry")
	} else if sb["enabled"] != false {
		t.Errorf("expected sandbox enabled=false, got %v", sb["enabled"])
	}

	// All other core modules default ON.
	for _, id := range []string{
		"identity", "public-gateway", "llm-gateway", "jobs", "secrets-vault",
		"storage", "notifications", "webhooks-in", "billing", "observability",
		"mcp-client",
	} {
		entry, ok := byID[id]
		if !ok {
			t.Errorf("missing module entry: %s", id)
			continue
		}
		if entry["enabled"] != true {
			t.Errorf("expected %s enabled=true, got %v", id, entry["enabled"])
		}
	}

	// workload_modules: empty array, not nil.
	wm, ok := body["workload_modules"].([]any)
	if !ok {
		t.Errorf("expected workload_modules array, got %T", body["workload_modules"])
	}
	if len(wm) != 0 {
		t.Errorf("expected empty workload_modules, got %d", len(wm))
	}

	// multi_tenancy_enabled mirrors the modules.multi-tenancy enabled state.
	mtEnabled, ok := body["multi_tenancy_enabled"].(bool)
	if !ok {
		t.Fatalf("expected multi_tenancy_enabled boolean, got %T", body["multi_tenancy_enabled"])
	}
	if mtEnabled {
		t.Errorf("expected multi_tenancy_enabled=false in v1 default, got true")
	}
}

func TestModulesStateRespectsConfigOverrides(t *testing.T) {
	cfg := config.Default()
	cfg.Modules.Enabled = map[string]bool{
		"multi-tenancy": true, // override default OFF
		"jobs":          false, // override default ON
	}
	cfg.Modules.Adapters = map[string]string{
		"storage": "s3",
	}
	cfg.Modules.WorkloadModules = []string{"agent-commerce"}

	srv := New(cfg, slog.Default(), Deps{})
	req := httptest.NewRequest("GET", "/api/v1/modules", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	mods, _ := body["modules"].([]any)
	byID := map[string]map[string]any{}
	for _, m := range mods {
		mm, _ := m.(map[string]any)
		id, _ := mm["id"].(string)
		byID[id] = mm
	}

	if byID["multi-tenancy"]["enabled"] != true {
		t.Errorf("expected multi-tenancy enabled=true via override")
	}
	if byID["jobs"]["enabled"] != false {
		t.Errorf("expected jobs enabled=false via override")
	}
	if byID["storage"]["adapter"] != "s3" {
		t.Errorf("expected storage adapter=s3, got %v", byID["storage"]["adapter"])
	}

	mtEnabled, _ := body["multi_tenancy_enabled"].(bool)
	if !mtEnabled {
		t.Errorf("expected multi_tenancy_enabled=true when override flips it on")
	}

	wm, _ := body["workload_modules"].([]any)
	if len(wm) != 1 || wm[0] != "agent-commerce" {
		t.Errorf("expected workload_modules=[agent-commerce], got %v", wm)
	}
}

// ─── /api/v1/queues/summary ───────────────────────────────────────────────

func TestQueueSummaryStub(t *testing.T) {
	s := newDashTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/queues/summary", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, k := range []string{"pending", "running", "failed", "succeeded_today"} {
		v, ok := body[k].(float64)
		if !ok {
			t.Errorf("expected %s to be a number, got %T", k, body[k])
			continue
		}
		if v != 0 {
			t.Errorf("expected %s=0 in stub, got %v", k, v)
		}
	}

	recent, ok := body["recent"].([]any)
	if !ok {
		t.Errorf("expected recent to be an array, got %T", body["recent"])
	}
	if len(recent) != 0 {
		t.Errorf("expected empty recent in stub, got %d", len(recent))
	}
}

// ─── helper coverage ──────────────────────────────────────────────────────

func TestUUIDString(t *testing.T) {
	// Canonical 8-4-4-4-12 format from a known byte pattern.
	u := pgtype.UUID{
		Bytes: [16]byte{
			0x01, 0x23, 0x45, 0x67,
			0x89, 0xab,
			0xcd, 0xef,
			0xfe, 0xdc,
			0xba, 0x98, 0x76, 0x54, 0x32, 0x10,
		},
		Valid: true,
	}
	got := uuidString(u)
	want := "01234567-89ab-cdef-fedc-ba9876543210"
	if got != want {
		t.Errorf("uuidString = %q, want %q", got, want)
	}

	// Invalid UUID returns empty string.
	if uuidString(pgtype.UUID{Valid: false}) != "" {
		t.Errorf("expected empty string for invalid UUID")
	}
}

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		code  int32
		valid bool
		want  string
	}{
		{200, true, "succeeded"},
		{299, true, "succeeded"},
		{300, true, "failed"},
		{500, true, "failed"},
		{0, false, "failed"}, // null status -> failed
	}
	for _, c := range cases {
		got := classifyStatus(pgtype.Int4{Int32: c.code, Valid: c.valid})
		if got != c.want {
			t.Errorf("classifyStatus(%d, valid=%v) = %q, want %q",
				c.code, c.valid, got, c.want)
		}
	}
}
