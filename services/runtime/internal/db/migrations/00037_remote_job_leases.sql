-- +goose Up
-- Language-neutral (remote) job workers — the pull-based lease protocol
-- (PRD R3). A remote (python/typescript) River job has no in-process Go
-- handler; instead the River executor registers a leasable "attempt" row
-- here and blocks on it (DB-backed rendezvous) while an out-of-process
-- worker leases the attempt over /api/v1/jobs/worker/*, runs it, and
-- reports completion / failure back. River retains durability + retry
-- semantics: lease-TTL expiry (a killed worker) surfaces to the executor
-- as a retryable error, and a permanent failure cancels the River job.
--
-- Both tables are tenant-owned: a worker key leases ONLY its own tenant's
-- attempts. tenant_id + FORCE ROW LEVEL SECURITY is the isolation boundary
-- at the database, independent of the app-level tenant filter (mirrors
-- 00004_rls.sql / 00032_memory_tenant_rls.sql). The River executor binds
-- app.tenant_id to the job's tenant before writing, and the worker HTTP
-- handlers inherit the resolver-bound tenant on their connection.

-- suite_job_leases — one row per (river job id, attempt). The rendezvous
-- point between the blocking River executor and the pull worker.
create table if not exists suite_job_leases (
  tenant_id uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  -- job_id is the River job id (river_job.id). No FK: River owns that
  -- table/schema and may live under a different search_path; the pair
  -- (job_id, attempt) is globally unique for our purposes.
  job_id bigint not null,
  attempt int not null,
  kind text not null,
  payload jsonb not null default '{}'::jsonb,
  -- state machine: ready -> leased -> completed | failed
  --                ready | leased -> superseded (a newer attempt landed)
  --                canceled is tracked via the boolean, not the state
  state text not null default 'ready'
    check (state in ('ready', 'leased', 'completed', 'failed', 'superseded')),
  worker_id text,
  lease_expires_at timestamptz,
  deadline timestamptz,
  result jsonb,
  error text,
  retryable boolean not null default false,
  canceled boolean not null default false,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (job_id, attempt)
);

-- Lease selection scans the oldest ready attempt for a tenant among the
-- kinds a worker declares.
create index if not exists suite_job_leases_lease_idx
  on suite_job_leases (tenant_id, state, kind, created_at);

alter table suite_job_leases enable row level security;
alter table suite_job_leases force row level security;

create policy tenant_isolation on suite_job_leases
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- suite_job_worker_logs — structured log lines a worker attaches to a run.
create table if not exists suite_job_worker_logs (
  id bigserial primary key,
  tenant_id uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  job_id bigint not null,
  attempt int not null,
  level text not null default 'info',
  message text not null default '',
  fields jsonb not null default '{}'::jsonb,
  at timestamptz not null default now()
);

create index if not exists suite_job_worker_logs_job_idx
  on suite_job_worker_logs (tenant_id, job_id, attempt, at);

alter table suite_job_worker_logs enable row level security;
alter table suite_job_worker_logs force row level security;

create policy tenant_isolation on suite_job_worker_logs
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_job_worker_logs;
alter table suite_job_worker_logs no force row level security;
alter table suite_job_worker_logs disable row level security;
drop index if exists suite_job_worker_logs_job_idx;
drop table if exists suite_job_worker_logs;

drop policy if exists tenant_isolation on suite_job_leases;
alter table suite_job_leases no force row level security;
alter table suite_job_leases disable row level security;
drop index if exists suite_job_leases_lease_idx;
drop table if exists suite_job_leases;
