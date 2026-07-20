// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const goodManifest = `id: notes
name: Notes
version: 0.1.0
description: Per-tenant notes.
enabled: true
migrations: migrations
resources:
  - name: notes
    fields:
      - name: title
        type: string
        required: true
      - name: body
        type: string
`

const goodMigration = `-- +goose Up
create table if not exists notes_entries (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid not null references suite_tenants(id) on delete cascade,
  body text not null,
  created_at timestamptz not null default now()
);
alter table notes_entries enable row level security;
alter table notes_entries force row level security;
create policy tenant_isolation on notes_entries
  using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
-- +goose Down
drop table if exists notes_entries;
`

// Contract: a well-formed module (valid manifest + tenant-isolated
// migration) validates clean.
func TestModuleDir_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "backai.module.yaml", goodManifest)
	writeFile(t, dir, "migrations/00001_init.sql", goodMigration)

	res := ModuleDir(dir)
	if !res.OK {
		t.Fatalf("expected valid module, got findings: %+v", res.Findings)
	}
	// The RLS lint should affirmatively confirm isolation.
	if !hasFinding(res, "ok", "tenant-isolated") {
		t.Fatalf("expected an RLS pass finding, got: %+v", res.Findings)
	}
}

// Contract: a tenant-owned table with NO row level security fails the
// multi-tenancy invariant.
func TestModuleDir_TenantTableWithoutRLSFails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "backai.module.yaml", goodManifest)
	writeFile(t, dir, "migrations/00001_init.sql", `-- +goose Up
create table if not exists leaky (
  id uuid primary key,
  tenant_id uuid not null,
  body text
);
`)
	res := ModuleDir(dir)
	if res.OK {
		t.Fatal("expected RLS-less tenant table to fail validation")
	}
	if !hasFinding(res, "error", "never enables row level security") {
		t.Fatalf("expected missing-RLS error, got: %+v", res.Findings)
	}
}

// Contract: RLS enabled but not FORCEd is a foot-gun and must fail.
func TestModuleDir_RLSNotForcedFails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "backai.module.yaml", goodManifest)
	writeFile(t, dir, "migrations/00001_init.sql", `-- +goose Up
create table if not exists t (
  id uuid primary key,
  tenant_id uuid not null
);
alter table t enable row level security;
create policy tenant_isolation on t using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
`)
	res := ModuleDir(dir)
	if res.OK || !hasFinding(res, "error", "does not FORCE") {
		t.Fatalf("expected FORCE-RLS error, got OK=%v findings=%+v", res.OK, res.Findings)
	}
}

// Contract: a table without tenant_id is not policed and does not trip the
// tenant lint.
func TestModuleDir_NonTenantTableIgnored(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "backai.module.yaml", goodManifest)
	writeFile(t, dir, "migrations/00001_init.sql", `-- +goose Up
create table if not exists global_lookup (
  id uuid primary key,
  label text not null
);
`)
	res := ModuleDir(dir)
	if !res.OK {
		t.Fatalf("non-tenant table should not fail RLS lint, got: %+v", res.Findings)
	}
}

// Contract: manifest shape errors (bad id, bad version, reserved/typo'd
// fields, no resources) are reported.
func TestModuleDir_BadManifest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "backai.module.yaml", `id: Bad_ID
name: ""
version: not-semver
resources:
  - name: notes
    fields:
      - name: tenant_id
        type: string
      - name: body
        type: markdown
`)
	res := ModuleDir(dir)
	if res.OK {
		t.Fatal("expected malformed manifest to fail")
	}
	for _, want := range []string{"id", "name is required", "semver", "reserved", "invalid type"} {
		if !hasFindingContains(res, want) {
			t.Fatalf("missing finding %q in %+v", want, res.Findings)
		}
	}
}

// Contract: the pre-PRD imperative manifest shape (routes:/meters:) is
// called out explicitly — the runtime cannot load it.
func TestModuleDir_ImperativeShapeRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "backai.module.yaml", `id: notes
name: Notes
version: 0.1.0
routes:
  - method: POST
    path: /notes
    handler: notes.Create
`)
	res := ModuleDir(dir)
	if res.OK {
		t.Fatal("expected imperative manifest to fail")
	}
	if !hasFindingContains(res, "declarative") {
		t.Fatalf("expected a declarative-shape pointer, got: %+v", res.Findings)
	}
}

// Contract: a resource-less manifest fails — declarative modules serve
// resources.
func TestModuleDir_NoResourcesFails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "backai.module.yaml", `id: notes
name: Notes
version: 0.1.0
`)
	res := ModuleDir(dir)
	if res.OK || !hasFindingContains(res, "at least one resource") {
		t.Fatalf("expected no-resources error, got OK=%v findings=%+v", res.OK, res.Findings)
	}
}

// Contract: the legacy manifest.yaml filename is still accepted.
func TestModuleDir_LegacyManifestName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "manifest.yaml", goodManifest)
	res := ModuleDir(dir)
	if !res.OK {
		t.Fatalf("legacy manifest.yaml should validate, got: %+v", res.Findings)
	}
}

// Contract: a missing manifest is an error.
func TestModuleDir_NoManifest(t *testing.T) {
	dir := t.TempDir()
	res := ModuleDir(dir)
	if res.OK || !hasFindingContains(res, "no module manifest") {
		t.Fatalf("expected no-manifest error, got: %+v", res.Findings)
	}
}

// Contract: a well-formed agent scaffold validates.
func TestAgentDir_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.py", `from agentfield import Agent
app = Agent(node_id="notes-assistant")
@app.reasoner(tags=["echo"])
async def echo(p): return p
`)
	writeFile(t, dir, "requirements.txt", "agentfield>=0.1.109\n")
	writeFile(t, dir, "Dockerfile", "FROM python:3.12-slim\n")
	res := AgentDir(dir)
	if !res.OK {
		t.Fatalf("expected valid agent, got: %+v", res.Findings)
	}
}

// Contract: an agent whose deps omit agentfield fails.
func TestAgentDir_MissingDep(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.py", `from agentfield import Agent
app = Agent(node_id="x")
@app.reasoner()
async def r(p): return p
`)
	writeFile(t, dir, "requirements.txt", "pydantic>=2\n")
	res := AgentDir(dir)
	if res.OK || !hasFindingContains(res, "does not pin agentfield") {
		t.Fatalf("expected missing-dep error, got: %+v", res.Findings)
	}
}

func TestFindModuleDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "workload-modules/notes/backai.module.yaml", goodManifest)
	writeFile(t, root, "modules/tasks/manifest.yaml", goodManifest)
	writeFile(t, root, "workload-modules/empty/.keep", "")
	dirs := FindModuleDirs(root)
	if len(dirs) != 2 {
		t.Fatalf("expected 2 module dirs, got %v", dirs)
	}
}

func hasFinding(r *Result, level, substr string) bool {
	for _, f := range r.Findings {
		if f.Level == level && strings.Contains(f.Message, substr) {
			return true
		}
	}
	return false
}

func hasFindingContains(r *Result, substr string) bool {
	for _, f := range r.Findings {
		if strings.Contains(f.Message, substr) {
			return true
		}
	}
	return false
}
