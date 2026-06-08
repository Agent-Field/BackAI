-- +goose Up
-- Phase 3 — Shipwright autonomous coding-agent factory.
--
-- AF Stack owns only the SaaS metadata: customer task rows and final patch
-- pointers. AgentField owns the AI-stateful execution graph, step logs,
-- harness tool calls, spans, traces, and memory. `run_id` stores the
-- AgentField execution/run handle used by the dashboard to link into the
-- AgentField deep view.

create table if not exists suite_shipwright_tasks (
  id          uuid primary key default gen_random_uuid(),
  tenant_id   uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  user_id     uuid references suite_users(id) on delete set null,
  title       text not null,
  description text not null,
  repo_url    text not null,
  status      text not null default 'queued'
    check (status in ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
  run_id      text,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);

create index if not exists suite_shipwright_tasks_tenant_time_idx
  on suite_shipwright_tasks (tenant_id, created_at desc);

create index if not exists suite_shipwright_tasks_run_idx
  on suite_shipwright_tasks (run_id)
  where run_id is not null;

create table if not exists suite_shipwright_patches (
  task_id    uuid not null references suite_shipwright_tasks(id) on delete cascade,
  ref        text not null,
  summary    text not null default '',
  diff_url   text,
  created_at timestamptz not null default now(),
  primary key (task_id, ref)
);

create index if not exists suite_shipwright_patches_task_time_idx
  on suite_shipwright_patches (task_id, created_at desc);

alter table suite_shipwright_tasks enable row level security;
alter table suite_shipwright_tasks force row level security;

create policy tenant_isolation on suite_shipwright_tasks
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

alter table suite_shipwright_patches enable row level security;
alter table suite_shipwright_patches force row level security;

create policy tenant_isolation on suite_shipwright_patches
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or exists (
      select 1
      from suite_shipwright_tasks t
      where t.id = task_id
        and (
          current_setting('app.bypass_rls', true) = 'on'
          or t.tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
        )
    )
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or exists (
      select 1
      from suite_shipwright_tasks t
      where t.id = task_id
        and (
          current_setting('app.bypass_rls', true) = 'on'
          or t.tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
        )
    )
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_shipwright_patches;
alter table suite_shipwright_patches no force row level security;
alter table suite_shipwright_patches disable row level security;

drop policy if exists tenant_isolation on suite_shipwright_tasks;
alter table suite_shipwright_tasks no force row level security;
alter table suite_shipwright_tasks disable row level security;

drop index if exists suite_shipwright_patches_task_time_idx;
drop table if exists suite_shipwright_patches;

drop index if exists suite_shipwright_tasks_run_idx;
drop index if exists suite_shipwright_tasks_tenant_time_idx;
drop table if exists suite_shipwright_tasks;
