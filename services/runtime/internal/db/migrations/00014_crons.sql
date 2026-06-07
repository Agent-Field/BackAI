-- +goose Up
-- Phase 12.2 — Cron schedules over the jobs queue.
--
-- A cron is a thin convenience layer over the existing jobs queue. Each
-- row says "every <schedule>, enqueue job <job_name> with <args>". A
-- single goroutine in the runtime ticks once a minute, claims rows whose
-- next_run_at <= now() under FOR UPDATE SKIP LOCKED, enqueues a job for
-- each, and rewrites next_run_at from the cron expression. The dashboard
-- mutates this table directly through /api/v1/crons; the scheduler picks
-- up changes on the next tick.
--
-- RLS mirrors the suite_skills shared-row pattern (tenant_id IS NULL
-- means the cron is visible to every tenant; the bypass GUC still wins
-- for admin listings).

create table if not exists suite_crons (
  id            uuid primary key default gen_random_uuid(),
  tenant_id     uuid,
  name          text not null,
  job_name      text not null,
  schedule      text not null,
  args          jsonb not null default '{}'::jsonb,
  is_active     boolean not null default true,
  last_run_at   timestamptz,
  next_run_at   timestamptz not null,
  created_at    timestamptz not null default now()
);

-- Scheduler hot-path: index covers the WHERE / ORDER BY of the
-- ClaimDue() query. is_active is a low-cardinality leading column but
-- pruning it from the index would force a Seq Scan as the table grows.
create index if not exists suite_crons_due_idx
  on suite_crons (is_active, next_run_at);

-- Listings: dashboard filters by tenant and orders newest-first.
create index if not exists suite_crons_tenant_created_idx
  on suite_crons (tenant_id, created_at desc);

alter table suite_crons enable row level security;
alter table suite_crons force row level security;

create policy tenant_isolation on suite_crons
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
drop policy if exists tenant_isolation on suite_crons;
alter table suite_crons no force row level security;
alter table suite_crons disable row level security;
drop index if exists suite_crons_tenant_created_idx;
drop index if exists suite_crons_due_idx;
drop table if exists suite_crons;
