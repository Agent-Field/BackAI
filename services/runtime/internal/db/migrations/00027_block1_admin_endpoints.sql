-- +goose Up
-- Block 1 admin endpoints: provider health, notification mutes, brand
-- overrides, and pg_stat_statements for DB health.

create extension if not exists pg_stat_statements;

-- +goose StatementBegin
do $$
begin
  execute format('grant pg_read_all_stats to %I', current_user);
exception
  when insufficient_privilege then
    raise warning 'could not grant pg_read_all_stats to %, DB health will warn about reduced stats visibility', current_user;
  when undefined_object then
    raise warning 'pg_read_all_stats role is not available on this Postgres installation';
end $$;
-- +goose StatementEnd

create table if not exists suite_provider_health_log (
  id uuid primary key default gen_random_uuid(),
  provider text not null,
  status text not null check (status in ('healthy','degraded','unhealthy','unknown')),
  latency_ms int not null default 0,
  observed_at timestamptz not null default now(),
  details jsonb not null default '{}'::jsonb
);

create index if not exists suite_provider_health_log_provider_time_idx
  on suite_provider_health_log (provider, observed_at desc);

create index if not exists suite_provider_health_log_time_idx
  on suite_provider_health_log (observed_at desc);

create table if not exists suite_notification_mutes (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid references suite_tenants(id) on delete cascade,
  kind text not null default '*',
  recipient text not null default '*',
  template text not null default '*',
  category text not null default '*',
  reason text,
  expires_at timestamptz,
  created_by text,
  created_at timestamptz not null default now()
);

create index if not exists suite_notification_mutes_tenant_idx
  on suite_notification_mutes (tenant_id, created_at desc);

create index if not exists suite_notification_mutes_expiry_idx
  on suite_notification_mutes (expires_at);

alter table suite_notification_mutes enable row level security;
alter table suite_notification_mutes force row level security;

create policy tenant_isolation on suite_notification_mutes
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id is null
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id is null
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

create table if not exists suite_brand_override (
  id boolean primary key default true,
  brand jsonb not null,
  updated_by text,
  updated_at timestamptz not null default now(),
  constraint suite_brand_override_singleton check (id = true)
);

-- +goose Down
drop table if exists suite_brand_override;
drop policy if exists tenant_isolation on suite_notification_mutes;
alter table suite_notification_mutes no force row level security;
alter table suite_notification_mutes disable row level security;
drop table if exists suite_notification_mutes;
drop table if exists suite_provider_health_log;
drop extension if exists pg_stat_statements;
