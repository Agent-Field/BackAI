-- +goose Up
-- Initial AF Stack schema. Adds the suite_* tables that every module needs.
-- Module-specific schema (multi-tenancy, billing, sandbox, etc.) lands as
-- separate migration files when those modules are implemented.

create extension if not exists "pgcrypto";

-- Tenants (used when multi-tenancy module is enabled; otherwise a single
-- "default" tenant row holds everything).
create table if not exists suite_tenants (
  id          uuid primary key default gen_random_uuid(),
  slug        text unique not null,
  name        text not null,
  plan        text default 'free',
  settings    jsonb default '{}'::jsonb,
  quota       jsonb default '{}'::jsonb,
  created_at  timestamptz not null default now(),
  deleted_at  timestamptz
);

-- Seed the default tenant. When MT is on, this row is still the fallback
-- for unscoped operations.
insert into suite_tenants (id, slug, name)
values ('00000000-0000-0000-0000-000000000000', 'default', 'Default')
on conflict (slug) do nothing;

-- Users (managed by better-auth integration in Phase 3).
create table if not exists suite_users (
  id          uuid primary key default gen_random_uuid(),
  email       text unique not null,
  name        text,
  avatar_url  text,
  created_at  timestamptz not null default now(),
  deleted_at  timestamptz
);

create index if not exists suite_users_email_idx on suite_users (email);

-- Memberships link users to tenants with a role.
create table if not exists suite_memberships (
  tenant_id   uuid references suite_tenants(id) on delete cascade,
  user_id     uuid references suite_users(id) on delete cascade,
  role        text not null check (role in ('owner','admin','member','viewer')),
  invited_at  timestamptz not null default now(),
  accepted_at timestamptz,
  primary key (tenant_id, user_id)
);

-- API keys: per-tenant, scoped, hashed secret.
create table if not exists suite_api_keys (
  id              uuid primary key default gen_random_uuid(),
  tenant_id       uuid references suite_tenants(id) on delete cascade,
  prefix          text unique not null,
  hashed_secret   text not null,
  name            text,
  scopes          text[] not null default '{}',
  created_by      uuid references suite_users(id),
  created_at      timestamptz not null default now(),
  last_used_at    timestamptz,
  expires_at      timestamptz,
  revoked_at      timestamptz
);

create index if not exists suite_api_keys_tenant_idx on suite_api_keys (tenant_id);

-- Gateway request log (high-volume; rotated separately in prod).
create table if not exists suite_gateway_requests (
  id              uuid primary key default gen_random_uuid(),
  tenant_id       uuid,
  api_key_id      uuid references suite_api_keys(id),
  user_id         uuid,
  endpoint        text not null,
  method          text not null,
  status_code     int,
  duration_ms     int,
  request_bytes   int,
  response_bytes  int,
  af_execution_id text,
  request_id      text,
  ip              inet,
  user_agent      text,
  created_at      timestamptz not null default now()
);

create index if not exists suite_gateway_requests_tenant_time_idx
  on suite_gateway_requests (tenant_id, created_at desc);
create index if not exists suite_gateway_requests_api_key_time_idx
  on suite_gateway_requests (api_key_id, created_at desc);
create index if not exists suite_gateway_requests_af_exec_idx
  on suite_gateway_requests (af_execution_id);

-- Audit log: suite-level events. AF emits its own verifiable credentials
-- separately; this captures CRUD on tenants, secrets, API keys, etc.
create table if not exists suite_audit_log (
  id              uuid primary key default gen_random_uuid(),
  tenant_id       uuid,
  user_id         uuid,
  api_key_id      uuid,
  action          text not null,
  resource_type   text,
  resource_id     text,
  metadata        jsonb default '{}'::jsonb,
  ip              inet,
  user_agent      text,
  occurred_at     timestamptz not null default now()
);

create index if not exists suite_audit_log_tenant_time_idx
  on suite_audit_log (tenant_id, occurred_at desc);
create index if not exists suite_audit_log_action_idx
  on suite_audit_log (action, occurred_at desc);

-- +goose Down
drop table if exists suite_audit_log;
drop table if exists suite_gateway_requests;
drop table if exists suite_api_keys;
drop table if exists suite_memberships;
drop table if exists suite_users;
drop table if exists suite_tenants;
