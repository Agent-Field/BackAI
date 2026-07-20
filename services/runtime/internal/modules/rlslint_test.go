// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"strings"
	"testing"
)

// notesMigrationSQL mirrors the shipped workload-modules/notes migration:
// the exact tenant-isolation shape the lint must accept.
const notesMigrationSQL = `
create table if not exists notes_notes (
  id         uuid        primary key default gen_random_uuid(),
  tenant_id  uuid        not null,
  title      text        not null,
  body       text        not null default '',
  done       boolean     not null default false,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
create index if not exists notes_notes_tenant_idx on notes_notes (tenant_id, created_at desc);
alter table notes_notes enable row level security;
alter table notes_notes force row level security;
create policy tenant_isolation on notes_notes
  using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
  with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
`

func TestLintMigrationSQL_AcceptsNotes(t *testing.T) {
	if err := LintMigrationSQL(notesMigrationSQL); err != nil {
		t.Fatalf("notes migration should pass the RLS lint, got: %v", err)
	}
}

func TestLintMigrationSQL_RejectsTenantlessTable(t *testing.T) {
	sql := `
create table widgets (
  id uuid primary key default gen_random_uuid(),
  label text not null
);
alter table widgets enable row level security;
alter table widgets force row level security;
create policy tenant_isolation on widgets using (true);
`
	err := LintMigrationSQL(sql)
	if err == nil {
		t.Fatal("tenantless table must be rejected")
	}
	if !strings.Contains(err.Error(), "tenant_id") {
		t.Fatalf("error should mention the missing tenant_id column: %v", err)
	}
}

func TestLintMigrationSQL_RejectsMissingIsolationStatements(t *testing.T) {
	base := `create table t1 (
  id uuid primary key,
  tenant_id uuid not null,
  name text
);`
	cases := map[string]string{
		"missing enable": base + `
alter table t1 force row level security;
create policy p on t1 using (true);`,
		"missing force": base + `
alter table t1 enable row level security;
create policy p on t1 using (true);`,
		"missing policy": base + `
alter table t1 enable row level security;
alter table t1 force row level security;`,
		"nothing at all": base,
	}
	for name, sql := range cases {
		t.Run(name, func(t *testing.T) {
			if err := LintMigrationSQL(sql); err == nil {
				t.Fatalf("case %q should be rejected", name)
			}
		})
	}
}

func TestLintMigrationSQL_NoTablesIsFine(t *testing.T) {
	// Index-only / data-only migration creates no table => nothing to isolate.
	sql := `create index if not exists foo_idx on some_existing_table (col);`
	if err := LintMigrationSQL(sql); err != nil {
		t.Fatalf("migration with no CREATE TABLE should pass, got: %v", err)
	}
}

func TestLintMigrationSQL_IgnoresComments(t *testing.T) {
	// A commented-out CREATE TABLE must not be treated as a real table (which
	// would demand RLS statements that aren't there).
	sql := `
-- create table ghost (id uuid, secret text);
/* create table ghost2 (id uuid); */
create table real_t (
  id uuid primary key,
  tenant_id uuid not null,
  v text
);
alter table real_t enable row level security;
alter table real_t force row level security;
create policy p on real_t using (true);
`
	if err := LintMigrationSQL(sql); err != nil {
		t.Fatalf("commented DDL should be ignored, got: %v", err)
	}
}

func TestLintMigrationSQL_SchemaQualifiedNames(t *testing.T) {
	sql := `
create table public.acct (
  id uuid primary key,
  tenant_id uuid not null,
  n text
);
alter table public.acct enable row level security;
alter table public.acct force row level security;
create policy tenant_isolation on public.acct using (true);
`
	if err := LintMigrationSQL(sql); err != nil {
		t.Fatalf("schema-qualified table should pass, got: %v", err)
	}
}
