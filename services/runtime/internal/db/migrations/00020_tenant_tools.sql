-- +goose Up
-- Phase 2 — first-party native tools (internal/tools), per-tenant enable.
--
-- This complements suite_tool_adapters (00019) — that table backs the
-- legacy /api/v1/tools/adapters monolithic surface. This new table
-- powers the strict-interface internal/tools/ package: one row per
-- (tenant, tool) where `tool` is one of the six canonical names
-- (browser, search, fs, exec, http, sql). The active adapter for each
-- tool type is picked via env (AF_STACK_TOOL_BROWSER, etc.) — this
-- table only stores tenant policy.

create table if not exists suite_tenant_tools (
  tenant_id uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  tool text not null check (
    tool in ('browser', 'search', 'fs', 'exec', 'http', 'sql')
  ),
  enabled boolean not null default false,
  config jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (tenant_id, tool)
);

create index if not exists suite_tenant_tools_tenant_enabled_idx
  on suite_tenant_tools (tenant_id, enabled);

alter table suite_tenant_tools enable row level security;
alter table suite_tenant_tools force row level security;

create policy tenant_isolation on suite_tenant_tools
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_tenant_tools;
alter table suite_tenant_tools no force row level security;
alter table suite_tenant_tools disable row level security;
drop index if exists suite_tenant_tools_tenant_enabled_idx;
drop table if exists suite_tenant_tools;
