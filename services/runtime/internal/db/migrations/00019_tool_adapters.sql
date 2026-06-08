-- +goose Up
-- Phase 2 — tenant-scoped built-in agent tool adapters.
--
-- This table stores policy/configuration only: which built-in adapters are
-- enabled for a tenant and any non-secret per-adapter config. AgentField owns
-- actual agent tool-call state, spans, and traces. AF Stack may write the
-- small call audit table below so operators can see adapter usage, but it is
-- not a trace/span store.

create table if not exists suite_tool_adapters (
  tenant_id uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  adapter text not null check (
    adapter in ('browser-use', 'searxng', 'fs', 'exec', 'http', 'sql')
  ),
  enabled boolean not null default false,
  config jsonb not null default '{}'::jsonb,
  updated_by uuid references suite_users(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (tenant_id, adapter)
);

create index if not exists suite_tool_adapters_tenant_enabled_idx
  on suite_tool_adapters (tenant_id, enabled);

create table if not exists suite_tool_adapter_calls (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  adapter text not null,
  tool text not null,
  status text not null check (status in ('succeeded', 'failed')),
  duration_ms int not null default 0,
  error text,
  created_at timestamptz not null default now()
);

create index if not exists suite_tool_adapter_calls_tenant_time_idx
  on suite_tool_adapter_calls (tenant_id, created_at desc);

alter table suite_tool_adapters enable row level security;
alter table suite_tool_adapters force row level security;

create policy tenant_isolation on suite_tool_adapters
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

alter table suite_tool_adapter_calls enable row level security;
alter table suite_tool_adapter_calls force row level security;

create policy tenant_isolation on suite_tool_adapter_calls
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_tool_adapter_calls;
alter table suite_tool_adapter_calls no force row level security;
alter table suite_tool_adapter_calls disable row level security;
drop index if exists suite_tool_adapter_calls_tenant_time_idx;
drop table if exists suite_tool_adapter_calls;

drop policy if exists tenant_isolation on suite_tool_adapters;
alter table suite_tool_adapters no force row level security;
alter table suite_tool_adapters disable row level security;
drop index if exists suite_tool_adapters_tenant_enabled_idx;
drop table if exists suite_tool_adapters;
