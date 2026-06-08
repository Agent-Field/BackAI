-- +goose Up
-- Phase 2 — tenant-scoped feature flags.
--
-- The runtime owns durable flag state so dashboard/customer apps and SDK
-- consumers can read the same values. This is general app config, not
-- AgentField state.

create table if not exists suite_feature_flags (
  tenant_id uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  key text not null,
  label text not null default '',
  description text not null default '',
  enabled boolean not null default false,
  metadata jsonb not null default '{}'::jsonb,
  updated_by uuid references suite_users(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (tenant_id, key)
);

create index if not exists suite_feature_flags_tenant_enabled_idx
  on suite_feature_flags (tenant_id, enabled);

alter table suite_feature_flags enable row level security;
alter table suite_feature_flags force row level security;

create policy tenant_isolation on suite_feature_flags
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_feature_flags;
alter table suite_feature_flags no force row level security;
alter table suite_feature_flags disable row level security;
drop index if exists suite_feature_flags_tenant_enabled_idx;
drop table if exists suite_feature_flags;
