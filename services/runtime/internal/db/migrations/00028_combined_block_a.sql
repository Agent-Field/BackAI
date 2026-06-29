-- +goose Up
-- Combined Block A: aggregation backing schema and durable operator state.

alter table suite_cost_events
  add column if not exists reasoner text,
  add column if not exists status_code int,
  add column if not exists error_code text;

create index if not exists suite_cost_events_reasoner_time_idx
  on suite_cost_events (tenant_id, agent, reasoner, occurred_at desc);

create table if not exists suite_tool_calls (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  agent_id text not null default 'system',
  tool_name text not null,
  transport text not null check (transport in ('native','mcp')),
  duration_ms int not null default 0,
  status text not null check (status in ('success','error','timeout')),
  error_code text,
  called_at timestamptz not null default now()
);

create index if not exists suite_tool_calls_tenant_time_idx
  on suite_tool_calls (tenant_id, called_at desc);

create index if not exists suite_tool_calls_tool_time_idx
  on suite_tool_calls (tool_name, transport, called_at desc);

alter table suite_tool_calls enable row level security;
alter table suite_tool_calls force row level security;

create policy tenant_isolation on suite_tool_calls
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

create table if not exists suite_notification_channels (
  id uuid primary key default gen_random_uuid(),
  kind text not null check (kind in ('email','sms','push','log')),
  config_json jsonb not null default '{}'::jsonb,
  enabled boolean not null default true,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (kind)
);

create index if not exists suite_notification_channels_enabled_idx
  on suite_notification_channels (enabled, kind);

create table if not exists suite_oauth_refresh_log (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  provider text not null,
  user_id uuid references suite_users(id) on delete set null,
  status text not null check (status in ('success','failed')),
  error_code text,
  attempted_at timestamptz not null default now()
);

create index if not exists suite_oauth_refresh_log_tenant_time_idx
  on suite_oauth_refresh_log (tenant_id, attempted_at desc);

create index if not exists suite_oauth_refresh_log_provider_time_idx
  on suite_oauth_refresh_log (provider, attempted_at desc);

alter table suite_oauth_refresh_log enable row level security;
alter table suite_oauth_refresh_log force row level security;

create policy tenant_isolation on suite_oauth_refresh_log
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

create table if not exists suite_sql_history (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  user_id uuid not null references suite_users(id) on delete cascade,
  query text not null,
  query_sha256 text not null,
  executed_second timestamptz not null,
  executed_at timestamptz not null default now(),
  unique (user_id, query_sha256, executed_second)
);

create index if not exists suite_sql_history_user_time_idx
  on suite_sql_history (user_id, executed_at desc);

alter table suite_sql_history enable row level security;
alter table suite_sql_history force row level security;

create policy user_visibility on suite_sql_history
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or user_id = nullif(current_setting('app.user_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or user_id = nullif(current_setting('app.user_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists user_visibility on suite_sql_history;
alter table suite_sql_history no force row level security;
alter table suite_sql_history disable row level security;
drop index if exists suite_sql_history_user_time_idx;
drop table if exists suite_sql_history;

drop policy if exists tenant_isolation on suite_oauth_refresh_log;
alter table suite_oauth_refresh_log no force row level security;
alter table suite_oauth_refresh_log disable row level security;
drop index if exists suite_oauth_refresh_log_provider_time_idx;
drop index if exists suite_oauth_refresh_log_tenant_time_idx;
drop table if exists suite_oauth_refresh_log;

drop index if exists suite_notification_channels_enabled_idx;
drop table if exists suite_notification_channels;

drop policy if exists tenant_isolation on suite_tool_calls;
alter table suite_tool_calls no force row level security;
alter table suite_tool_calls disable row level security;
drop index if exists suite_tool_calls_tool_time_idx;
drop index if exists suite_tool_calls_tenant_time_idx;
drop table if exists suite_tool_calls;

drop index if exists suite_cost_events_reasoner_time_idx;
alter table suite_cost_events
  drop column if exists error_code,
  drop column if exists status_code,
  drop column if exists reasoner;
