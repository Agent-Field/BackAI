-- +goose Up
-- Row Level Security policies for the multi-tenancy module.
--
-- Phase 6.1 introduces RLS as the per-tenant isolation primitive:
-- domain tables (suite_secrets, suite_gateway_requests, suite_audit_log)
-- become invisible to queries whose session does NOT have
-- app.tenant_id set to the row's tenant_id.
--
-- The tenant context is set per request by the resolver middleware in
-- services/runtime/internal/server/server.go via:
--     select set_config('app.tenant_id', '<uuid>', true)
-- which scopes the binding to the current transaction (`is_local = true`).
--
-- Admin escape hatch:
-- Cross-tenant admin queries (e.g. ListTenants, audit log views) need
-- to see rows across all tenants. Rather than rely on a Postgres
-- superuser role (which the runtime should not run as), we use a
-- documented GUC `app.bypass_rls` that policies accept as an OR-ed
-- bypass. The admin handlers call set_config('app.bypass_rls', 'on',
-- true) inside a transaction to opt in. The default is 'off' so a
-- forgotten or missing setting still applies tenant isolation.
--
-- Why not just BYPASSRLS on the role?
-- BYPASSRLS would skip RLS for ALL queries on the role — including
-- handlers that haven't explicitly opted in. The GUC approach forces
-- each admin code path to opt in transactionally, which is far safer
-- for code review (`grep SetBypassRLS` finds every bypass site).
--
-- Tables that intentionally skip RLS:
--   - suite_tenants     — admins list/create across tenants
--   - suite_users       — global directory; per-tenant scoping is via
--                         suite_memberships
--   - suite_memberships — read by admins for the tenant detail page;
--                         we restrict via app code, not RLS
--   - suite_api_keys    — admin-managed; the resolver looks up keys
--                         across tenants before any tenant context exists
--
-- Backfill:
-- tenant_id columns already exist on the policed tables (see migration
-- 00001_init.sql). Any pre-existing rows with NULL tenant_id are
-- back-filled to the default tenant so RLS doesn't hide them after
-- enable.

-- Step 1: backfill nulls to the default tenant uuid so existing
-- (Phase 4/5) rows remain visible once the policies activate.
update suite_gateway_requests
   set tenant_id = '00000000-0000-0000-0000-000000000000'
 where tenant_id is null;

update suite_audit_log
   set tenant_id = '00000000-0000-0000-0000-000000000000'
 where tenant_id is null;

-- Step 2: switch the tenant_id columns to NOT NULL with a DEFAULT so
-- callers that omit tenant_id on insert still land in the right row.
-- suite_secrets already has NOT NULL on tenant_id from migration 00003.
alter table suite_gateway_requests
  alter column tenant_id set default '00000000-0000-0000-0000-000000000000',
  alter column tenant_id set not null;

alter table suite_audit_log
  alter column tenant_id set default '00000000-0000-0000-0000-000000000000',
  alter column tenant_id set not null;

-- Step 3: enable RLS and define the per-tenant policy on each table.
--
-- The policy reads the GUC `app.tenant_id`; current_setting(name, true)
-- returns the empty string when the GUC isn't set. Casting an empty
-- string to uuid is invalid, so we guard with NULLIF and treat the
-- absent setting as "no rows match" (defence: a forgotten resolver
-- shouldn't expose every tenant's data).
--
-- The bypass clause OR-es in true when app.bypass_rls = 'on'. The
-- admin handlers set this transactionally before issuing cross-tenant
-- queries; everything else gets tenant-scoped output.

-- suite_secrets ------------------------------------------------------
alter table suite_secrets enable row level security;
-- FORCE makes RLS apply even to table owners, which closes a foot-gun
-- where running migrations as the table owner would silently bypass.
alter table suite_secrets force row level security;

create policy tenant_isolation on suite_secrets
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- suite_gateway_requests ---------------------------------------------
alter table suite_gateway_requests enable row level security;
alter table suite_gateway_requests force row level security;

create policy tenant_isolation on suite_gateway_requests
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- suite_audit_log ----------------------------------------------------
alter table suite_audit_log enable row level security;
alter table suite_audit_log force row level security;

create policy tenant_isolation on suite_audit_log
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
-- Order matters: drop policies before disabling RLS, then revert the
-- NOT NULL / DEFAULT changes so the schema returns to its pre-00004
-- shape.

drop policy if exists tenant_isolation on suite_audit_log;
alter table suite_audit_log no force row level security;
alter table suite_audit_log disable row level security;

drop policy if exists tenant_isolation on suite_gateway_requests;
alter table suite_gateway_requests no force row level security;
alter table suite_gateway_requests disable row level security;

drop policy if exists tenant_isolation on suite_secrets;
alter table suite_secrets no force row level security;
alter table suite_secrets disable row level security;

alter table suite_audit_log
  alter column tenant_id drop not null,
  alter column tenant_id drop default;

alter table suite_gateway_requests
  alter column tenant_id drop not null,
  alter column tenant_id drop default;
