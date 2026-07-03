-- +goose Up
-- Turnkey billing — the plans catalog.
--
-- suite_billing_plans is the operator-editable pricing catalog: each row
-- names a plan, optionally binds it to a Stripe Price (stripe_price_id),
-- declares the per-tenant monthly LLM budget the runtime enforces (402
-- BUDGET_EXCEEDED past it), and carries a freeform entitlements JSON
-- object that app code / SDKs read via GET /api/v1/billing/entitlements
-- (e.g. {"simulations": 3}).
--
-- suite_billing_customers.plan (00011) references plans by id (soft — no
-- FK, so deleting a plan never strands a tenant; unknown plan ids resolve
-- to the default plan at read time).
--
-- Not tenant-scoped data — the catalog is global, like suite_budgets. No
-- RLS: reads are public (pricing pages need it), writes are operator-only
-- at the API layer (rbac ResourceAdminBilling).

create table if not exists suite_billing_plans (
  id               text primary key,          -- slug, e.g. 'free', 'pro'
  name             text not null,
  stripe_price_id  text,                      -- null = not purchasable via Stripe (e.g. free)
  price_usd_month  double precision not null default 0,  -- display price
  llm_budget_usd   double precision,          -- null = no enforced budget
  entitlements     jsonb not null default '{}'::jsonb,
  is_default       boolean not null default false,
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now()
);

create unique index if not exists suite_billing_plans_default_idx
  on suite_billing_plans (is_default) where is_default;

create index if not exists suite_billing_plans_price_idx
  on suite_billing_plans (stripe_price_id) where stripe_price_id is not null;

-- Seed a free default so entitlement reads always resolve. Operators
-- edit or replace this from the dashboard's Platform → Billing page.
insert into suite_billing_plans (id, name, price_usd_month, entitlements, is_default)
values ('free', 'Free', 0, '{}'::jsonb, true)
on conflict (id) do nothing;

-- suite_billing_settings holds the operator-panel billing configuration
-- (Stripe secret key + webhook secret), AES-GCM envelope-encrypted with
-- the same KMS-backed cipher as the tenant secrets vault. Global rows —
-- not tenant data (the vault's tenant FK is why this is a separate
-- table). Env vars remain an override for infra-as-code deployments.
create table if not exists suite_billing_settings (
  key         text primary key,
  value_enc   bytea not null,
  updated_at  timestamptz not null default now()
);

-- +goose Down
drop table if exists suite_billing_settings;
drop index if exists suite_billing_plans_price_idx;
drop index if exists suite_billing_plans_default_idx;
drop table if exists suite_billing_plans;
