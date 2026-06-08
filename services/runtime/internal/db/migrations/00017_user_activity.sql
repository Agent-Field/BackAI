-- +goose Up
-- Phase 2 — tenant-scoped user activity log.
--
-- This records customer/product activity for the app built on AF Stack:
-- signups, uploads, searches, billing-page views, domain actions, etc.
-- It is intentionally separate from suite_audit_log (operator/admin
-- compliance events) and from AgentField memory/runs/spans/traces.

create table if not exists suite_user_activity (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  user_id uuid references suite_users(id) on delete set null,
  api_key_id uuid references suite_api_keys(id) on delete set null,
  actor_type text not null default 'system'
    check (actor_type in ('user', 'api_key', 'system', 'anonymous')),
  action text not null,
  resource_type text,
  resource_id text,
  metadata jsonb not null default '{}'::jsonb,
  ip inet,
  user_agent text,
  occurred_at timestamptz not null default now()
);

create index if not exists suite_user_activity_tenant_time_idx
  on suite_user_activity (tenant_id, occurred_at desc);

create index if not exists suite_user_activity_user_time_idx
  on suite_user_activity (tenant_id, user_id, occurred_at desc);

create index if not exists suite_user_activity_action_time_idx
  on suite_user_activity (tenant_id, action, occurred_at desc);

create index if not exists suite_user_activity_resource_idx
  on suite_user_activity (tenant_id, resource_type, resource_id, occurred_at desc);

alter table suite_user_activity enable row level security;
alter table suite_user_activity force row level security;

create policy tenant_isolation on suite_user_activity
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_user_activity;
alter table suite_user_activity no force row level security;
alter table suite_user_activity disable row level security;
drop index if exists suite_user_activity_resource_idx;
drop index if exists suite_user_activity_action_time_idx;
drop index if exists suite_user_activity_user_time_idx;
drop index if exists suite_user_activity_tenant_time_idx;
drop table if exists suite_user_activity;
