// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func rlsMigration(table string) string {
	return `create table if not exists ` + table + ` (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid not null,
  title text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
alter table ` + table + ` enable row level security;
alter table ` + table + ` force row level security;
create policy tenant_isolation on ` + table + ` using (true) with check (true);`
}

func writeModule(t *testing.T, root, dir, manifest, migration string) {
	t.Helper()
	moduleDir := filepath.Join(root, dir)
	writeFile(t, moduleDir, ManifestFilename, manifest)
	if migration != "" {
		writeFile(t, filepath.Join(moduleDir, "migrations"), "00001_init.sql", migration)
	}
}

const activeManifest = `
id: active
name: Active
version: 0.1.0
enabled: false
resources:
  - name: items
    fields:
      - {name: title, type: string, required: true}
`

const inactiveManifest = `
id: inactive
name: Inactive
version: 0.1.0
enabled: false
resources:
  - name: items
    fields:
      - {name: title, type: string, required: true}
`

// tenantless has a valid manifest and is enabled, but its migration creates
// a table with no tenant_id — the lint must disable it anyway.
const tenantlessManifest = `
id: tenantless
name: Tenantless
version: 0.1.0
enabled: true
resources:
  - name: rows
    fields:
      - {name: v, type: string}
`

const badManifest = `
id: bad
name: Bad
version: 0.1.0
` // no resources => invalid

func loadFixture(t *testing.T) *Manager {
	t.Helper()
	root := t.TempDir()
	writeModule(t, root, "active", activeManifest, rlsMigration("active_items"))
	writeModule(t, root, "inactive", inactiveManifest, rlsMigration("inactive_items"))
	writeModule(t, root, "tenantless", tenantlessManifest,
		`create table tenantless_rows (id uuid primary key, v text);`)
	writeModule(t, root, "bad", badManifest, "")
	// A directory without a manifest is not a module and must be ignored.
	writeFile(t, filepath.Join(root, "notamodule"), "readme.txt", "hi")

	return Load(root, []string{"active", "tenantless"}, testLogger())
}

func findLoaded(m *Manager, id string) *Loaded {
	for _, l := range m.Modules() {
		if l.Manifest != nil && l.Manifest.ID == id {
			return l
		}
	}
	return nil
}

func TestLoad_ClassifiesModules(t *testing.T) {
	m := loadFixture(t)

	active := findLoaded(m, "active")
	if active == nil || !active.Active || active.LoadErr != nil {
		t.Fatalf("active module should load + be active: %+v", active)
	}
	if active.Migration != MigrationPending {
		t.Fatalf("active migration state = %q, want pending", active.Migration)
	}

	inactive := findLoaded(m, "inactive")
	if inactive == nil || inactive.Active {
		t.Fatalf("inactive module should be discovered but not active: %+v", inactive)
	}
	if inactive.Migration != MigrationSkipped {
		t.Fatalf("inactive migration state = %q, want skipped", inactive.Migration)
	}

	tenantless := findLoaded(m, "tenantless")
	if tenantless == nil || tenantless.LoadErr == nil {
		t.Fatalf("tenantless module must be disabled by the RLS lint: %+v", tenantless)
	}
	if tenantless.routable() {
		t.Fatal("tenantless module (even though enabled) must not be routable")
	}

	// The bad manifest has no id, so look it up among modules with a LoadErr.
	var bad *Loaded
	for _, l := range m.Modules() {
		if l.Manifest == nil && l.LoadErr != nil {
			bad = l
		}
	}
	if bad == nil {
		t.Fatal("invalid manifest should be recorded with a LoadErr and skipped")
	}
}

func TestMount_OnlyRoutableModules(t *testing.T) {
	m := loadFixture(t)
	m.SetDB(&fakeQuerier{})
	mux := http.NewServeMux()
	mounted := m.Mount(mux, nil)

	// Exactly one routable module (active) with one resource => 5 routes.
	if len(mounted) != 5 {
		t.Fatalf("expected 5 mounted routes, got %d: %v", len(mounted), mounted)
	}

	// Active resource routes resolve.
	for _, target := range []string{
		"/api/v1/workload/active/items",
		"/api/v1/workload/active/items/abc",
	} {
		req, _ := http.NewRequest("GET", target, nil)
		if _, pattern := mux.Handler(req); pattern == "" {
			t.Fatalf("expected a handler for %s", target)
		}
	}

	// Inactive + tenantless modules contribute NO routes.
	for _, target := range []string{
		"/api/v1/workload/inactive/items",
		"/api/v1/workload/tenantless/rows",
	} {
		req, _ := http.NewRequest("GET", target, nil)
		if _, pattern := mux.Handler(req); pattern != "" {
			t.Fatalf("disabled module leaked a route: %s (pattern %q)", target, pattern)
		}
	}
}

func TestAdminList_ReportsHealth(t *testing.T) {
	m := loadFixture(t)
	byID := map[string]AdminModuleView{}
	var errored int
	for _, v := range m.AdminList() {
		if v.ID != "" {
			byID[v.ID] = v
		}
		if v.Health == "error" {
			errored++
		}
	}
	if byID["active"].Health != "ok" {
		t.Fatalf("active health = %q, want ok", byID["active"].Health)
	}
	if byID["inactive"].Health != "disabled" {
		t.Fatalf("inactive health = %q, want disabled", byID["inactive"].Health)
	}
	if byID["tenantless"].Health != "error" || byID["tenantless"].Error == "" {
		t.Fatalf("tenantless should be error with a message: %+v", byID["tenantless"])
	}
	// tenantless + bad manifest are both errored.
	if errored < 2 {
		t.Fatalf("expected >=2 errored modules, got %d", errored)
	}
}

func TestLoad_MissingRootIsEmpty(t *testing.T) {
	m := Load(filepath.Join(t.TempDir(), "nope"), nil, testLogger())
	if len(m.Modules()) != 0 {
		t.Fatalf("missing root should yield no modules, got %d", len(m.Modules()))
	}
	mux := http.NewServeMux()
	if got := m.Mount(mux, nil); len(got) != 0 {
		t.Fatalf("no routes should mount, got %v", got)
	}
	if len(m.AdminList()) != 0 {
		t.Fatal("admin list should be empty")
	}
}

func TestLoad_DuplicateID(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "a-copy1", activeManifest, rlsMigration("active_items"))
	writeModule(t, root, "a-copy2", activeManifest, rlsMigration("active_items"))
	m := Load(root, []string{"active"}, testLogger())
	var errs int
	for _, l := range m.Modules() {
		if l.LoadErr != nil {
			errs++
		}
	}
	if errs != 1 {
		t.Fatalf("exactly one of the duplicate-id modules should error, got %d", errs)
	}
}

// TestShippedNotesModule verifies the committed reference module loads and
// lints clean (the living example must always be valid).
func TestShippedNotesModule(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..", "..", "..", "workload-modules")
	if _, err := os.Stat(filepath.Join(root, "notes", ManifestFilename)); err != nil {
		t.Skipf("shipped notes module not found at %s: %v", root, err)
	}
	m := Load(root, []string{"notes"}, testLogger())
	notes := findLoaded(m, "notes")
	if notes == nil {
		t.Fatal("notes module not discovered")
	}
	if notes.LoadErr != nil {
		t.Fatalf("shipped notes module must load clean, got: %v", notes.LoadErr)
	}
	if !notes.Active || notes.Migration != MigrationPending {
		t.Fatalf("notes should be active + pending (no DB), got active=%v state=%q", notes.Active, notes.Migration)
	}
	if names := notes.Manifest.ResourceNames(); len(names) != 1 || names[0] != "notes" {
		t.Fatalf("unexpected resources: %v", names)
	}
}

func TestApplyMigrations_DBBoundSkip(t *testing.T) {
	t.Skip("Manager.ApplyMigrations requires a live Postgres; verified live downstream")
}
