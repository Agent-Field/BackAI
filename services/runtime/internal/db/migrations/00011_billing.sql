-- +goose Up
-- Phase 10.4 — Stripe billing: customer mirror + per-tenant meter aggregation.
--
-- This migration creates two tables that together back the Stripe-first
-- billing surface:
--
--   suite_billing_customers — one row per tenant that has (or will have) a
--     Stripe Customer. The row mirrors the relevant subscription fields so
--     the dashboard's billing tab doesn't need to round-trip to Stripe on
--     every page load. The row is upserted by the webhook handler in
--     services/runtime/internal/billing/webhook_handler.go whenever Stripe
--     pushes customer.subscription.updated / invoice.payment_succeeded.
--
--   suite_usage_meters — one row per (meter, tenant_id, period_start)
--     bucket. Each row accumulates quantity (additive) and, when the meter
--     has a price attached, cost_usd. The Meter() service method UPSERTs
--     into this table whenever a sandbox run finishes, an LLM call is
--     billed, etc. The dashboard's billing tab reads this directly via
--     GET /api/v1/billing/meters.
--
-- The "free" plan default avoids null sentinels — every existing tenant
-- without a Stripe Customer is implicitly on free. trial_ends_at,
-- current_period_end, subscription_status are nullable because they only
-- have meaning once Stripe has been hooked up.
--
-- RLS follows the same pattern as 00004_rls.sql: per-tenant policy gated
-- on app.tenant_id GUC, with an admin escape hatch via app.bypass_rls=on.
-- The dashboard's admin handlers set bypass_rls so the customers/meters
-- listings work across tenants; SDK calls keep the tenant scope.

create table if not exists suite_billing_customers (
  tenant_id           uuid primary key,
  stripe_customer_id  text,
  email               text,
  plan                text not null default 'free',
  trial_ends_at       timestamptz,
  current_period_end  timestamptz,
  subscription_status text,
  created_at          timestamptz not null default now(),
  updated_at          timestamptz not null default now()
);

create index if not exists suite_billing_customers_plan_idx
  on suite_billing_customers (tenant_id, plan);

create table if not exists suite_usage_meters (
  meter           text not null,
  tenant_id       uuid not null,
  period_start    timestamptz not null,
  period_end      timestamptz not null,
  quantity        numeric not null default 0,
  cost_usd        numeric,
  stripe_meter_id text,
  last_synced_at  timestamptz,
  primary key (meter, tenant_id, period_start)
);

create index if not exists suite_usage_meters_tenant_period_idx
  on suite_usage_meters (tenant_id, period_start desc);

-- RLS — per-tenant isolation with admin bypass via app.bypass_rls.
-- Mirror of the pattern documented in 00004_rls.sql.

alter table suite_billing_customers enable row level security;
alter table suite_billing_customers force row level security;

create policy tenant_isolation on suite_billing_customers
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

alter table suite_usage_meters enable row level security;
alter table suite_usage_meters force row level security;

create policy tenant_isolation on suite_usage_meters
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_usage_meters;
alter table suite_usage_meters no force row level security;
alter table suite_usage_meters disable row level security;

drop policy if exists tenant_isolation on suite_billing_customers;
alter table suite_billing_customers no force row level security;
alter table suite_billing_customers disable row level security;

drop index if exists suite_usage_meters_tenant_period_idx;
drop table if exists suite_usage_meters;

drop index if exists suite_billing_customers_plan_idx;
drop table if exists suite_billing_customers;
