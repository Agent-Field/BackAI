-- Reference workload-module migration (PRD R2).
--
-- Table name follows the <module>_<resource> convention: the `notes`
-- module's `notes` resource is backed by `notes_notes`. The runtime
-- statically lints this file before applying it and REFUSES any module
-- whose CREATE TABLE lacks a tenant_id column, ENABLE + FORCE ROW LEVEL
-- SECURITY, and a CREATE POLICY — the same tenant-isolation contract the
-- platform's own tables use (see internal/db/migrations/00004_rls.sql).

create table if not exists notes_notes (
  id         uuid        primary key default gen_random_uuid(),
  tenant_id  uuid        not null,
  title      text        not null,
  body       text        not null default '',
  done       boolean     not null default false,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists notes_notes_tenant_idx
  on notes_notes (tenant_id, created_at desc);

alter table notes_notes enable row level security;
-- FORCE so RLS applies even to the table owner (closes the migration-role
-- bypass foot-gun).
alter table notes_notes force row level security;

create policy tenant_isolation on notes_notes
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );
