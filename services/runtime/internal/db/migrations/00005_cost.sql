-- +goose Up
-- Phase 7 — per-call LLM cost ledger + per-tenant monthly budgets.
--
-- suite_cost_events is the canonical per-call log. The LLM gateway
-- (Phase 7.1) writes one row per upstream call via
-- internal/cost.Recorder; the dashboard's /api/v1/cost summary
-- aggregates this table.
--
-- Decision: NO row-level-security on either table in Phase 7.
--
-- Phase 6's RLS scope (migration 00004) intentionally excluded
-- admin-managed tables — the cost summary and budget admin endpoints
-- must see across tenants to render the operator's "billable customers"
-- and "budget exhaustion" pages. Tenant-scoped reads happen at the
-- query layer (WHERE tenant_id = $1) rather than via session GUCs.
--
-- suite_budgets is the per-tenant cap. One row per tenant, primary
-- key on tenant_id keeps Set() idempotent: PUT becomes ON CONFLICT.

create table if not exists suite_cost_events (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid references suite_tenants(id) on delete set null,
  api_key_id uuid references suite_api_keys(id) on delete set null,
  model text not null,
  provider text not null,
  agent text,
  prompt_tokens int not null default 0,
  completion_tokens int not null default 0,
  total_tokens int not null default 0,
  cost_usd numeric(12,6) not null default 0,
  cached boolean not null default false,
  latency_ms int not null default 0,
  occurred_at timestamptz not null default now()
);
create index suite_cost_events_tenant_time_idx on suite_cost_events (tenant_id, occurred_at desc);
create index suite_cost_events_model_time_idx on suite_cost_events (model, occurred_at desc);

create table if not exists suite_budgets (
  tenant_id uuid primary key references suite_tenants(id) on delete cascade,
  monthly_usd numeric(12,2) not null,
  alert_threshold_pct int not null default 80 check (alert_threshold_pct between 0 and 100),
  current_period_start timestamptz not null default date_trunc('month', now()),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

-- +goose Down
drop table if exists suite_budgets;
drop table if exists suite_cost_events;
