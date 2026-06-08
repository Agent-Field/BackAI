// SPDX-License-Identifier: Apache-2.0

package tooladapters

import "testing"

func TestCatalogContainsRequiredAdapters(t *testing.T) {
	defs := Catalog(Config{DatabaseAvailable: true, SandboxAvailable: true})
	got := map[AdapterID]AdapterDefinition{}
	for _, def := range defs {
		got[def.ID] = def
	}
	for _, id := range []AdapterID{
		AdapterBrowserUse,
		AdapterSearXNG,
		AdapterFS,
		AdapterExec,
		AdapterHTTP,
		AdapterSQL,
	} {
		def, ok := got[id]
		if !ok {
			t.Fatalf("catalog missing %s", id)
		}
		if len(def.Tools) == 0 {
			t.Fatalf("catalog adapter %s has no tools", id)
		}
	}
	if !got[AdapterHTTP].DefaultEnabled {
		t.Fatal("http should default enabled")
	}
	if !got[AdapterSQL].Configured {
		t.Fatal("sql should be configured when DatabaseAvailable=true")
	}
	if !got[AdapterExec].Configured || !got[AdapterFS].Configured {
		t.Fatal("exec/fs should be configured when SandboxAvailable=true")
	}
}

func TestValidateReadOnlySQL(t *testing.T) {
	valid := []string{
		"select 1",
		"select slug, name from suite_tenants;",
		"with recent as (select 1 as n) select n from recent",
		"explain select 1",
	}
	for _, sql := range valid {
		if _, err := validateReadOnlySQL(sql); err != nil {
			t.Fatalf("expected %q valid: %v", sql, err)
		}
	}

	invalid := []string{
		"",
		"delete from suite_tenants",
		"select 1; delete from suite_tenants",
		"insert into x values (1)",
		"set app.bypass_rls = on",
	}
	for _, sql := range invalid {
		if _, err := validateReadOnlySQL(sql); err == nil {
			t.Fatalf("expected %q invalid", sql)
		}
	}
}
