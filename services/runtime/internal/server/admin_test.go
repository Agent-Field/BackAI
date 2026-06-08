// SPDX-License-Identifier: Apache-2.0

// admin_test.go — handler-level tests for the Phase 6 multi-tenancy admin
// surface.
//
// These tests exercise the gating + wire-shape contract WITHOUT a real
// Postgres-backed tenancy.Manager. Two configurations cover the matrix
// the dashboard cares about:
//
//   1. multi-tenancy module DISABLED (default) — every endpoint must
//      return 503 with the standard envelope {error:{code:"MT_DISABLED"}}.
//   2. multi-tenancy module ENABLED but Server.Deps.Tenancy is nil
//      (Phase 6.1 boot mode) — every endpoint returns 503
//      {error:{code:"MT_NOT_CONFIGURED"}}.
//
// The "full happy path" — manager wired, real tenant create/list — lives
// in the tenancy package's DB-backed integration tests; replicating it
// here would require a live Postgres, which is out of scope for the
// server-handler unit suite.
//
// The wire-shape assertions still pin every field name from
// apps/dashboard/src/lib/api.ts so a snake_case typo here breaks Go tests
// before it breaks the dashboard's zod safeParse.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

// newAdminDisabledServer builds a Server with the multi-tenancy module OFF
// (the v1 default). Used to verify the MT_DISABLED gate.
func newAdminDisabledServer(t *testing.T) *Server {
	t.Helper()
	return newBareTestServer(t)
}

// newAdminEnabledNoMgrServer builds a Server with the multi-tenancy module
// flag ON but no Tenancy manager wired. Used to verify the
// MT_NOT_CONFIGURED gate.
func newAdminEnabledNoMgrServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.Default()
	if cfg.Modules.Enabled == nil {
		cfg.Modules.Enabled = map[string]bool{}
	}
	cfg.Modules.Enabled["multi-tenancy"] = true
	return New(cfg, slog.Default(), Deps{})
}

// adminEndpoints enumerates every endpoint registered by
// registerAdminRoutes(). Used as a table so a missing gate or a typo in
// the route registration is loud.
type endpointCase struct {
	method string
	path   string
	body   string
}

func adminEndpoints() []endpointCase {
	return []endpointCase{
		{"GET", "/api/v1/admin/tenants", ""},
		{"POST", "/api/v1/admin/tenants", `{"slug":"acme","name":"Acme"}`},
		{"GET", "/api/v1/admin/tenants/t-1", ""},
		{"GET", "/api/v1/admin/tenants/t-1/drilldown", ""},
		{"PATCH", "/api/v1/admin/tenants/t-1", `{"name":"New"}`},
		{"DELETE", "/api/v1/admin/tenants/t-1", ""},

		{"GET", "/api/v1/admin/users", ""},

		{"GET", "/api/v1/admin/memberships", ""},
		{"POST", "/api/v1/admin/memberships", `{"tenant_id":"t","user_id":"u","role":"member"}`},
		{"DELETE", "/api/v1/admin/memberships/t-1/u-1", ""},

		{"GET", "/api/v1/admin/keys", ""},
		{"POST", "/api/v1/admin/keys", `{"tenant_id":"t","scopes":[]}`},
		{"DELETE", "/api/v1/admin/keys/k-1", ""},
		{"GET", "/api/v1/admin/keys/k-1/spend", ""},

		{"GET", "/api/v1/admin/audit", ""},
	}
}

// TestAdminEndpointsReturn503WhenMTDisabled walks every admin endpoint
// and asserts the MT_DISABLED envelope. Failures here mean either the
// gate skipped a handler or the envelope shape drifted.
func TestAdminEndpointsReturn503WhenMTDisabled(t *testing.T) {
	s := newAdminDisabledServer(t)
	for _, ec := range adminEndpoints() {
		t.Run(ec.method+" "+ec.path, func(t *testing.T) {
			var body *strings.Reader
			if ec.body != "" {
				body = strings.NewReader(ec.body)
			}
			req := newRequestMaybeBody(ec.method, ec.path, body)
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
			}
			assertErrorEnvelope(t, rec.Body.Bytes(), "MT_DISABLED")
		})
	}
}

// TestAdminEndpointsReturn503WhenManagerMissing pins the second gate —
// the module flag is on but Server.Deps.Tenancy is still nil. This is
// the boot-mode the dashboard hits on a fresh runtime that hasn't run
// the migration to seed tenants yet.
func TestAdminEndpointsReturn503WhenManagerMissing(t *testing.T) {
	s := newAdminEnabledNoMgrServer(t)
	for _, ec := range adminEndpoints() {
		t.Run(ec.method+" "+ec.path, func(t *testing.T) {
			var body *strings.Reader
			if ec.body != "" {
				body = strings.NewReader(ec.body)
			}
			req := newRequestMaybeBody(ec.method, ec.path, body)
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
			}
			assertErrorEnvelope(t, rec.Body.Bytes(), "MT_NOT_CONFIGURED")
		})
	}
}

// TestAdminTenantWireShapeMatchesDashboardContract validates the
// snake_case JSON tags on the Go wire structs by marshalling a fixture
// and verifying every field the zod TenantSchema declares is present.
// This pins the contract without needing the handler to execute.
func TestAdminTenantWireShapeMatchesDashboardContract(t *testing.T) {
	fixture := tenantWire{
		ID:       "t-1",
		Slug:     "acme",
		Name:     "Acme",
		Plan:     "free",
		Settings: map[string]interface{}{},
		Quota:    map[string]interface{}{},
		// CreatedAt is a string in the wire shape; pin a fixed value so
		// drift between RFC3339 / RFC3339Nano is caught.
		CreatedAt: "2026-06-06T12:00:00Z",
		DeletedAt: nil,
	}
	raw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	required := []string{"id", "slug", "name", "plan", "settings", "quota", "created_at", "deleted_at"}
	for _, k := range required {
		if _, ok := parsed[k]; !ok {
			t.Errorf("tenantWire missing field %q in JSON output: %s", k, raw)
		}
	}
	// deleted_at must serialise as explicit JSON null (zod nullable).
	if got, ok := parsed["deleted_at"]; !ok || got != nil {
		t.Errorf("deleted_at should be JSON null, got %v / present=%v", got, ok)
	}
}

// TestAdminAPIKeyWireExcludesValueOnList pins the one-time-reveal
// contract: APIKeySchema has NO `value` field; only IssuedAPIKeySchema
// has it. A future drift that accidentally projected the plaintext into
// the list response would be a critical leak.
func TestAdminAPIKeyWireExcludesValueOnList(t *testing.T) {
	k := apiKeyWire{
		ID:        "k-1",
		TenantID:  "t-1",
		Prefix:    "abc",
		Scopes:    []string{"read"},
		CreatedAt: "2026-06-06T12:00:00Z",
	}
	raw, err := json.Marshal(k)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"value"`) {
		t.Errorf("apiKeyWire must NOT include `value` — only IssuedAPIKey does. raw=%s", raw)
	}
}

// TestAdminIssuedKeyWireIncludesValueOnce confirms the IssuedAPIKey
// shape carries `value`. POST /api/v1/admin/keys serialises through
// this struct; subsequent GETs use apiKeyWire (no value).
func TestAdminIssuedKeyWireIncludesValueOnce(t *testing.T) {
	k := issuedKeyWire{
		apiKeyWire: apiKeyWire{
			ID:        "k-1",
			TenantID:  "t-1",
			Prefix:    "abc",
			Scopes:    []string{"read"},
			CreatedAt: "2026-06-06T12:00:00Z",
		},
		Value: "af_abc_secret",
	}
	raw, err := json.Marshal(k)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, _ := parsed["value"].(string); v != "af_abc_secret" {
		t.Errorf("expected value field on IssuedAPIKey wire, got %v", parsed["value"])
	}
	if _, ok := parsed["id"]; !ok {
		t.Errorf("IssuedAPIKey must embed APIKey fields; id missing")
	}
}

// TestAdminTenantDetailWireShape pins the joined tenant detail envelope
// (Tenant + members + api_keys + usage). Drift here breaks the
// dashboard's tenant detail page.
func TestAdminTenantDetailWireShape(t *testing.T) {
	d := tenantDetailWire{
		Tenant: tenantWire{
			ID: "t-1", Slug: "acme", Name: "Acme", Plan: "free",
			Settings: map[string]interface{}{}, Quota: map[string]interface{}{},
			CreatedAt: "2026-06-06T12:00:00Z",
		},
		Members: []tenantDetailMemberWire{
			{User: userWire{ID: "u-1", Email: "a@b.co", CreatedAt: "2026-06-06T12:00:00Z"}, Role: "owner"},
		},
		APIKeys: []apiKeyWire{},
		Usage: tenantDetailUsageWire{
			Requests30D: 12, CostUSD30D: 1.25, StorageBytes: 1024, SecretsCount: 3,
		},
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"tenant", "members", "api_keys", "usage"} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("tenantDetailWire missing %q in JSON: %s", k, raw)
		}
	}
	usage, _ := parsed["usage"].(map[string]any)
	for _, k := range []string{"requests_30d", "cost_usd_30d", "storage_bytes", "secrets_count"} {
		if _, ok := usage[k]; !ok {
			t.Errorf("usage missing %q in JSON: %s", k, raw)
		}
	}
}

// TestAdminTenantDrilldownWireShape pins the Phase 12.1 drilldown
// envelope. Drift here breaks the dashboard's /customers/tenants/[id]
// page (TenantDrilldownSchema in apps/dashboard/src/lib/api.ts).
func TestAdminTenantDrilldownWireShape(t *testing.T) {
	lastActive := "2026-06-06T12:00:00Z"
	subStatus := "active"
	periodEnd := "2026-07-01T00:00:00Z"
	d := tenantDrilldownWire{
		Tenant: tenantWire{
			ID: "t-1", Slug: "acme", Name: "Acme", Plan: "pro",
			Settings: map[string]interface{}{}, Quota: map[string]interface{}{},
			CreatedAt: "2026-06-06T12:00:00Z",
		},
		Members: []tenantDrilldownMemberWire{
			{
				User:         userWire{ID: "u-1", Email: "a@b.co", CreatedAt: "2026-06-06T12:00:00Z"},
				Role:         "owner",
				LastActiveAt: &lastActive,
			},
		},
		APIKeys: []apiKeyWire{},
		Usage: tenantDrilldownUsageWire{
			Requests30D:      42,
			CostUSD30D:       1.25,
			StorageBytes:     2048,
			SecretsCount:     3,
			CostSparkline:    make([]float64, 24),
			RequestSparkline: make([]float64, 24),
		},
		RecentRuns: []tenantDrilldownRunWire{
			{
				ID: "r-1", Agent: "sample.echo", Status: "succeeded",
				StartedAt: "2026-06-06T12:00:00Z", DurationMS: 120, CostUSD: 0.001,
			},
		},
		RecentWebhooks: []tenantDrilldownWebhookWire{
			{
				ID: "w-1", Direction: "inbound", EventType: "issue.opened",
				Status: "delivered", CreatedAt: "2026-06-06T12:00:00Z",
			},
		},
		Billing: &tenantDrilldownBillingWire{
			Plan:               "pro",
			SubscriptionStatus: &subStatus,
			CurrentPeriodEnd:   &periodEnd,
			TrialEndsAt:        nil,
		},
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"tenant", "members", "api_keys", "usage", "recent_runs", "recent_webhooks", "billing"} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("tenantDrilldownWire missing %q in JSON: %s", k, raw)
		}
	}

	// Usage must carry the two 24-element sparklines so zod's
	// .length(24) check passes.
	usage, _ := parsed["usage"].(map[string]any)
	for _, k := range []string{"requests_30d", "cost_usd_30d", "storage_bytes", "secrets_count", "cost_sparkline", "request_sparkline"} {
		if _, ok := usage[k]; !ok {
			t.Errorf("drilldown usage missing %q in JSON: %s", k, raw)
		}
	}
	costSpark, _ := usage["cost_sparkline"].([]any)
	if len(costSpark) != 24 {
		t.Errorf("cost_sparkline len = %d, want 24", len(costSpark))
	}
	reqSpark, _ := usage["request_sparkline"].([]any)
	if len(reqSpark) != 24 {
		t.Errorf("request_sparkline len = %d, want 24", len(reqSpark))
	}

	// Members must carry last_active_at (nullable, but present).
	members, _ := parsed["members"].([]any)
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	m0, _ := members[0].(map[string]any)
	for _, k := range []string{"user", "role", "last_active_at"} {
		if _, ok := m0[k]; !ok {
			t.Errorf("drilldown member missing %q in JSON: %s", k, raw)
		}
	}

	// recent_runs row shape.
	runs, _ := parsed["recent_runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	r0, _ := runs[0].(map[string]any)
	for _, k := range []string{"id", "agent", "status", "started_at", "duration_ms", "cost_usd"} {
		if _, ok := r0[k]; !ok {
			t.Errorf("drilldown run missing %q in JSON: %s", k, raw)
		}
	}

	// recent_webhooks row shape.
	hooks, _ := parsed["recent_webhooks"].([]any)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(hooks))
	}
	h0, _ := hooks[0].(map[string]any)
	for _, k := range []string{"id", "direction", "event_type", "status", "created_at"} {
		if _, ok := h0[k]; !ok {
			t.Errorf("drilldown webhook missing %q in JSON: %s", k, raw)
		}
	}

	// billing snapshot fields when present.
	billing, _ := parsed["billing"].(map[string]any)
	for _, k := range []string{"plan", "subscription_status", "current_period_end", "trial_ends_at"} {
		if _, ok := billing[k]; !ok {
			t.Errorf("drilldown billing missing %q in JSON: %s", k, raw)
		}
	}
	// trial_ends_at must serialise as explicit JSON null (zod nullable).
	if got, ok := billing["trial_ends_at"]; !ok || got != nil {
		t.Errorf("trial_ends_at should be JSON null, got %v / present=%v", got, ok)
	}
}

// TestAdminTenantDrilldownNilBillingSerialisesAsNull pins the contract
// for tenants without a suite_billing_customers row — the dashboard
// expects `billing: null` so the "Open Billing Portal" card stays hidden.
func TestAdminTenantDrilldownNilBillingSerialisesAsNull(t *testing.T) {
	d := tenantDrilldownWire{
		Tenant: tenantWire{
			ID: "t-1", Slug: "acme", Name: "Acme", Plan: "free",
			Settings: map[string]interface{}{}, Quota: map[string]interface{}{},
			CreatedAt: "2026-06-06T12:00:00Z",
		},
		Members:        []tenantDrilldownMemberWire{},
		APIKeys:        []apiKeyWire{},
		Usage:          tenantDrilldownUsageWire{CostSparkline: make([]float64, 24), RequestSparkline: make([]float64, 24)},
		RecentRuns:     []tenantDrilldownRunWire{},
		RecentWebhooks: []tenantDrilldownWebhookWire{},
		Billing:        nil,
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, ok := parsed["billing"]; !ok || got != nil {
		t.Errorf("billing should be JSON null when absent, got %v / present=%v", got, ok)
	}
}

// TestAdminAuditListWireShape pins the paginated audit envelope.
func TestAdminAuditListWireShape(t *testing.T) {
	a := auditListWire{
		Entries: []auditEntryWire{
			{
				ID:         "a-1",
				Action:     "tenant.created",
				Metadata:   map[string]interface{}{},
				OccurredAt: "2026-06-06T12:00:00Z",
			},
		},
		Total:   1,
		HasMore: false,
	}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"entries", "total", "has_more"} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("auditListWire missing %q in JSON: %s", k, raw)
		}
	}
	entries, _ := parsed["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry, _ := entries[0].(map[string]any)
	for _, k := range []string{"id", "tenant_id", "user_id", "api_key_id", "action",
		"resource_type", "resource_id", "metadata", "occurred_at"} {
		if _, ok := entry[k]; !ok {
			t.Errorf("auditEntryWire missing %q in JSON: %s", k, raw)
		}
	}
}

// TestAdminMembershipListWireShape pins the membership list envelope.
func TestAdminMembershipListWireShape(t *testing.T) {
	m := membershipListWire{
		Memberships: []membershipWire{
			{TenantID: "t-1", UserID: "u-1", Role: "owner", InvitedAt: "2026-06-06T12:00:00Z"},
		},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := parsed["memberships"].([]any); !ok {
		t.Errorf("memberships field missing or wrong type: %s", raw)
	}
}

// TestAdminUserListWireShape pins the user list envelope.
func TestAdminUserListWireShape(t *testing.T) {
	u := userListWire{
		Users: []userWire{
			{ID: "u-1", Email: "a@b.co", CreatedAt: "2026-06-06T12:00:00Z"},
		},
	}
	raw, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := parsed["users"].([]any); !ok {
		t.Errorf("users field missing or wrong type: %s", raw)
	}
}

// newRequestMaybeBody is a tiny helper that constructs a request with or
// without a JSON body. Avoids nil-vs-empty-reader gotchas in the table
// driver above.
func newRequestMaybeBody(method, path string, body *strings.Reader) *http.Request {
	if body == nil {
		return httptest.NewRequest(method, path, nil)
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	return req
}
