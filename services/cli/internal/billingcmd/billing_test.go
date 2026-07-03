// SPDX-License-Identifier: Apache-2.0

package billingcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/cli/internal/client"
)

// Contract: --entitlement k=v parses numbers as numbers, bools as bools,
// and everything else as strings, so the JSON matches how apps read it.
func TestEntitlementFlag_Typing(t *testing.T) {
	e := entitlementFlags{}
	for _, kv := range []string{"simulations=500", "beta=true", "tier=gold"} {
		if err := e.Set(kv); err != nil {
			t.Fatalf("Set(%q): %v", kv, err)
		}
	}
	if e["simulations"] != float64(500) {
		t.Errorf("simulations = %#v, want 500 (number)", e["simulations"])
	}
	if e["beta"] != true {
		t.Errorf("beta = %#v, want true (bool)", e["beta"])
	}
	if e["tier"] != "gold" {
		t.Errorf("tier = %#v, want \"gold\"", e["tier"])
	}
	if err := e.Set("noequals"); err == nil {
		t.Error("expected error for a value without '='")
	}
}

// testClient points a client.Client at a stub server.
func testClient(t *testing.T, h http.HandlerFunc) (*client.Client, func()) {
	t.Helper()
	srv := httptest.NewServer(h)
	return &client.Client{BaseURL: srv.URL, HTTP: srv.Client()}, srv.Close
}

// Contract: `plan set` sends the typed entitlements + fields on the wire
// as the runtime's upsert body.
func TestPlanSet_WireBody(t *testing.T) {
	var got map[string]any
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/admin/billing/plans" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "pro", "price_usd_month": 29.0})
	})
	defer done()

	var out bytes.Buffer
	err := runPlanSet(context.Background(), c,
		[]string{"--id", "pro", "--name", "Pro", "--price", "29", "--budget", "25",
			"--entitlement", "simulations=500", "--default"},
		&out, io.Discard)
	if err != nil {
		t.Fatalf("runPlanSet: %v", err)
	}
	if got["id"] != "pro" || got["name"] != "Pro" {
		t.Errorf("id/name wrong: %#v", got)
	}
	if got["price_usd_month"] != 29.0 || got["llm_budget_usd"] != 25.0 {
		t.Errorf("price/budget wrong: %#v", got)
	}
	if got["is_default"] != true {
		t.Errorf("is_default = %#v", got["is_default"])
	}
	ent, _ := got["entitlements"].(map[string]any)
	if ent["simulations"] != float64(500) {
		t.Errorf("entitlements = %#v", got["entitlements"])
	}
}

// Contract: omitting --budget leaves llm_budget_usd off the payload
// (meaning "no enforced budget"), rather than sending 0.
func TestPlanSet_OmitsBudgetWhenUnset(t *testing.T) {
	var got map[string]any
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "free"})
	})
	defer done()
	if err := runPlanSet(context.Background(), c,
		[]string{"--id", "free", "--name", "Free"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("runPlanSet: %v", err)
	}
	if _, present := got["llm_budget_usd"]; present {
		t.Errorf("llm_budget_usd should be omitted when --budget unset, got %#v", got["llm_budget_usd"])
	}
}

// Contract: a provisioning failure on a paid plan is enriched with the
// set-key / dashboard guidance.
func TestPlanSetError_GuidesToKey(t *testing.T) {
	base := &client.APIError{Status: 400, Code: "VALIDATION_FAILED",
		Message: "provision stripe price for plan \"pro\": no api key"}
	err := planSetError(base, 29)
	if !strings.Contains(err.Error(), "billing set-key") {
		t.Fatalf("expected set-key guidance, got: %v", err)
	}
	// A free plan's unrelated error is passed through untouched.
	if got := planSetError(base, 0); strings.Contains(got.Error(), "billing set-key") {
		t.Fatalf("free-plan error should not get key guidance: %v", got)
	}
}

// Contract: `status` reports mode and, when not live, points at the
// dashboard billing page.
func TestStatus_DevModePointsToDashboard(t *testing.T) {
	c, done := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(settingsStatus{
			Adapter: "stripe", Mode: "stub", Source: "none", SettingsWritable: true,
		})
	})
	defer done()
	var out bytes.Buffer
	if err := runStatus(context.Background(), c, &out); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "not live") || !strings.Contains(s, "/platform/billing") {
		t.Fatalf("dev-mode status should guide to the dashboard:\n%s", s)
	}
}
