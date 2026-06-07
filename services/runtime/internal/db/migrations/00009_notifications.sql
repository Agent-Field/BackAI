-- +goose Up
-- Phase 10.1 — suite notifications outbox.
--
-- A row in suite_notifications represents a single delivery attempt for a
-- notification (email today; sms/push/log later). The lifecycle is:
--
--   queued   -> inserted by the API/SDK; not yet picked up
--   sending  -> worker picked it up; adapter call in flight
--   sent     -> adapter returned success; provider_message_id populated
--   failed   -> adapter returned error; last_error populated
--   skipped  -> intentionally not sent (e.g. dry-run, dedup, tenant disabled)
--
-- The worker drains queued rows whose scheduled_at <= now() in (status,
-- scheduled_at) order. The dashboard reads per-tenant by (tenant_id,
-- created_at desc) so we index both query shapes.
--
-- RLS follows the same pattern as 00004_rls.sql / 00008_sandbox.sql: a
-- per-tenant policy keyed on `app.tenant_id`, with an admin bypass via
-- `app.bypass_rls = 'on'`. tenant_id is nullable so system / unscoped
-- notifications (e.g. "test from CLI") still persist.

create table if not exists suite_notifications (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid references suite_tenants(id) on delete set null,
  kind text not null check (kind in ('email','sms','push','log')),
  -- Adapter that handled (or will handle) this notification.
  -- nullable until the worker picks it up so callers can defer adapter
  -- selection (e.g. fall back to log in dev when no Resend key set).
  adapter text,
  template text not null,
  -- "to" is reserved in postgres so quote it. Lowercase column name keeps
  -- the wire shape stable with the dashboard's snake_case schema.
  "to" text not null,
  "from" text,
  subject text,
  data jsonb not null default '{}'::jsonb,
  status text not null default 'queued'
    check (status in ('queued','sending','sent','failed','skipped')),
  provider_message_id text,
  attempts int not null default 0,
  last_error text,
  scheduled_at timestamptz not null default now(),
  sent_at timestamptz,
  created_at timestamptz not null default now()
);

-- Worker drain order: status='queued' AND scheduled_at <= now().
create index suite_notifications_status_sched_idx
  on suite_notifications (status, scheduled_at);

-- Dashboard list: per-tenant newest first.
create index suite_notifications_tenant_time_idx
  on suite_notifications (tenant_id, created_at desc);

-- Row Level Security — mirrors the suite_secrets / suite_audit_log policy.
alter table suite_notifications enable row level security;
alter table suite_notifications force row level security;

create policy tenant_isolation on suite_notifications
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

-- +goose Down
drop policy if exists tenant_isolation on suite_notifications;
alter table suite_notifications no force row level security;
alter table suite_notifications disable row level security;
drop index if exists suite_notifications_tenant_time_idx;
drop index if exists suite_notifications_status_sched_idx;
drop table if exists suite_notifications;
