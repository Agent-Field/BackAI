-- +goose Up
-- Phase 9.1 — suite sandboxes.
--
-- The sandbox runs table holds one row per code-execution sandbox
-- invocation. A row is inserted at status='queued' before the adapter
-- starts the container, then transitions to 'running' once the container
-- is alive, then to one of 'done' / 'failed' / 'timeout' / 'killed' on
-- completion. exit_code, duration_s, resource counters, and the
-- stdout/stderr storage URLs are populated at the terminal transition.
--
-- tenant_id is FK with ON DELETE SET NULL so deleting a tenant doesn't
-- vaporise the historical ledger; workspace_id is a free-form string
-- (UUID, slug, or empty) the caller picks to group runs.
--
-- The indexes are tuned for the dashboard's two main queries:
--   1. recent runs per tenant — (tenant_id, created_at desc)
--   2. "what's running now" / status filter — (status, created_at desc)

create table if not exists suite_sandbox_runs (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid references suite_tenants(id) on delete set null,
  workspace_id text,
  adapter text not null,
  image text not null,
  command jsonb not null,
  status text not null check (status in ('queued','running','done','failed','timeout','killed')),
  exit_code int,
  duration_s int,
  cpu_seconds numeric(12,3),
  memory_peak_mb int,
  network_bytes_in bigint,
  network_bytes_out bigint,
  cost_usd numeric(12,6),
  stdout_url text,
  stderr_url text,
  artifacts_url text,
  started_at timestamptz,
  ended_at timestamptz,
  created_at timestamptz not null default now()
);
create index suite_sandbox_runs_tenant_time_idx on suite_sandbox_runs (tenant_id, created_at desc);
create index suite_sandbox_runs_status_idx on suite_sandbox_runs (status, created_at desc);

-- +goose Down
drop table if exists suite_sandbox_runs;
