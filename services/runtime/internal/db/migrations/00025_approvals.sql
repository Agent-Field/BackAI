-- +goose Up
-- Phase 3 — general human approval primitive.
--
-- This is deliberately not AgentField run state. It is tenant-scoped business
-- workflow metadata that any AF Stack workload can request and operators can
-- decide. AgentField-owned run approvals continue to live in AgentField.

create table if not exists suite_approvals (
  id           uuid primary key default gen_random_uuid(),
  tenant_id    uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  requested_by uuid references suite_users(id) on delete set null,
  kind         text not null,
  payload      jsonb not null default '{}'::jsonb,
  status       text not null default 'pending'
    check (status in ('pending', 'approved', 'denied', 'cancelled')),
  decided_by   uuid references suite_users(id) on delete set null,
  decided_at   timestamptz,
  decision_note text,
  created_at   timestamptz not null default now(),
  updated_at   timestamptz not null default now()
);

create index if not exists suite_approvals_tenant_status_time_idx
  on suite_approvals (tenant_id, status, created_at desc);

create index if not exists suite_approvals_kind_time_idx
  on suite_approvals (tenant_id, kind, created_at desc);

alter table suite_approvals enable row level security;
alter table suite_approvals force row level security;

create policy tenant_isolation on suite_approvals
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_approvals;
alter table suite_approvals no force row level security;
alter table suite_approvals disable row level security;
drop index if exists suite_approvals_kind_time_idx;
drop index if exists suite_approvals_tenant_status_time_idx;
drop table if exists suite_approvals;
