// SPDX-License-Identifier: Apache-2.0

package prodcheck

import (
	"strings"
	"testing"
)

func TestCheckDBRole(t *testing.T) {
	cases := []struct {
		name     string
		super    bool
		bypass   bool
		wantFail bool
	}{
		{"restricted role passes", false, false, false},
		{"superuser fails", true, false, true},
		{"bypassrls fails", false, true, true},
		{"both fails", true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckDBRole("app_runtime", tc.super, tc.bypass)
			if got.Failed() != tc.wantFail {
				t.Errorf("CheckDBRole failed=%v, want %v (%+v)", got.Failed(), tc.wantFail, got)
			}
			if got.Code != CodeDBRoleBypassesRLS {
				t.Errorf("unexpected code %q", got.Code)
			}
		})
	}
}

func TestCheckTenantRLS(t *testing.T) {
	t.Run("all tenant tables enabled+forced passes", func(t *testing.T) {
		tables := []TableRLS{
			{Table: "suite_memory", HasTenantID: true, RLSEnabled: true, RLSForced: true},
			{Table: "suite_cost_events", HasTenantID: true, RLSEnabled: true, RLSForced: true},
			// A non-tenant table (no tenant_id) is ignored even without RLS.
			{Table: "goose_db_version", HasTenantID: false, RLSEnabled: false, RLSForced: false},
		}
		got := CheckTenantRLS(tables)
		if got.Failed() {
			t.Errorf("expected pass, got fail: %+v", got)
		}
	})

	t.Run("tenant table with RLS enabled but not forced fails", func(t *testing.T) {
		tables := []TableRLS{
			{Table: "suite_secrets", HasTenantID: true, RLSEnabled: true, RLSForced: false},
		}
		got := CheckTenantRLS(tables)
		if !got.Failed() {
			t.Fatalf("expected fail, got: %+v", got)
		}
		if !strings.Contains(got.Detail, "suite_secrets") {
			t.Errorf("detail should name the offending table, got %q", got.Detail)
		}
	})

	t.Run("tenant table with RLS disabled fails", func(t *testing.T) {
		tables := []TableRLS{
			{Table: "suite_webhooks", HasTenantID: true, RLSEnabled: false, RLSForced: false},
		}
		got := CheckTenantRLS(tables)
		if !got.Failed() {
			t.Fatalf("expected fail, got: %+v", got)
		}
	})

	t.Run("no tenant tables at all passes vacuously", func(t *testing.T) {
		tables := []TableRLS{
			{Table: "goose_db_version", HasTenantID: false},
		}
		got := CheckTenantRLS(tables)
		if got.Failed() {
			t.Errorf("expected pass with no tenant tables, got %+v", got)
		}
	})
}

func TestCheckSecretsKMS(t *testing.T) {
	cases := []struct {
		name       string
		configured bool
		dev        bool
		wantFail   bool
	}{
		{"real key configured passes", true, false, false},
		{"unconfigured fails", false, false, true},
		{"configured but dev cipher fails", true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckSecretsKMS(tc.configured, tc.dev)
			if got.Failed() != tc.wantFail {
				t.Errorf("failed=%v want %v (%+v)", got.Failed(), tc.wantFail, got)
			}
		})
	}
}

func TestCheckCORS(t *testing.T) {
	cases := []struct {
		name     string
		origins  []string
		wantFail bool
	}{
		{"explicit origins pass", []string{"https://app.example.com", "https://dash.example.com"}, false},
		{"empty passes", nil, false},
		{"wildcard fails", []string{"https://app.example.com", "*"}, true},
		{"wildcard with surrounding space fails", []string{" * "}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckCORS(tc.origins)
			if got.Failed() != tc.wantFail {
				t.Errorf("failed=%v want %v (%+v)", got.Failed(), tc.wantFail, got)
			}
		})
	}
}

func TestCheckStorageIsolation(t *testing.T) {
	cases := []struct {
		name       string
		configured bool
		mt         bool
		wantStatus Status
	}{
		{"no storage -> skip", false, false, StatusSkip},
		{"storage + mt -> pass", true, true, StatusPass},
		{"storage without mt -> fail", true, false, StatusFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckStorageIsolation(tc.configured, tc.mt)
			if got.Status != tc.wantStatus {
				t.Errorf("status=%v want %v (%+v)", got.Status, tc.wantStatus, got)
			}
		})
	}
}

func TestCheckSandboxNetwork(t *testing.T) {
	cases := []struct {
		name     string
		policy   string
		wantFail bool
	}{
		{"isolated passes", "isolated", false},
		{"empty (defaults isolated) passes", "", false},
		{"ISOLATED case-insensitive passes", "ISOLATED", false},
		{"open fails", "open", true},
		{"restricted fails", "restricted", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckSandboxNetwork(tc.policy)
			if got.Failed() != tc.wantFail {
				t.Errorf("failed=%v want %v (%+v)", got.Failed(), tc.wantFail, got)
			}
		})
	}
}

func TestEvaluateFullyHardenedPasses(t *testing.T) {
	in := Inputs{
		DBRoleName:  "app_runtime",
		DBSuperuser: false,
		DBBypassRLS: false,
		Tables: []TableRLS{
			{Table: "suite_memory", HasTenantID: true, RLSEnabled: true, RLSForced: true},
		},
		CORSOrigins:          []string{"https://app.example.com"},
		KMSConfigured:        true,
		KMSDevMode:           false,
		StorageConfigured:    true,
		MultiTenancyEnabled:  true,
		SandboxNetworkPolicy: "isolated",
	}
	rep := Evaluate(in)
	if !rep.OK() {
		t.Fatalf("expected OK report, got failures: %+v", rep.Failures())
	}
	if rep.FirstFailureCode() != "" {
		t.Errorf("expected no failure code, got %q", rep.FirstFailureCode())
	}
}

func TestEvaluateUnhardenedReportsEveryFailure(t *testing.T) {
	in := Inputs{
		DBRoleName:  "postgres",
		DBSuperuser: true,
		Tables: []TableRLS{
			{Table: "suite_memory", HasTenantID: true, RLSEnabled: false, RLSForced: false},
		},
		CORSOrigins:          []string{"*"},
		KMSConfigured:        false,
		StorageConfigured:    true,
		MultiTenancyEnabled:  false,
		SandboxNetworkPolicy: "open",
	}
	rep := Evaluate(in)
	if rep.OK() {
		t.Fatal("expected failing report")
	}
	gotCodes := map[string]bool{}
	for _, f := range rep.Failures() {
		gotCodes[f.Code] = true
	}
	wantCodes := []string{
		CodeDBRoleBypassesRLS,
		CodeTenantTableRLSMissing,
		CodeSecretsDevKey,
		CodeCORSWildcardCreds,
		CodeStorageNotIsolated,
		CodeSandboxNetworkOpen,
	}
	for _, c := range wantCodes {
		if !gotCodes[c] {
			t.Errorf("expected failure code %q to be present", c)
		}
	}
	// FirstFailureCode is deterministic (evaluation order): DB role is first.
	if rep.FirstFailureCode() != CodeDBRoleBypassesRLS {
		t.Errorf("FirstFailureCode = %q, want %q", rep.FirstFailureCode(), CodeDBRoleBypassesRLS)
	}
}
