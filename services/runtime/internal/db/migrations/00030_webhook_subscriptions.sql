-- +goose Up
-- Tenant-owned OUTBOUND webhook subscriptions. Distinct from
-- suite_webhook_endpoints (which are INBOUND receivers keyed by slug): a
-- subscription is a URL a tenant registers to RECEIVE its own domain
-- events. The runtime fans out native, signed deliveries to a tenant's
-- active subscriptions when the tenant emits an event
-- (POST /api/v1/webhooks/emit). tenant_id is NOT NULL — a subscription
-- always belongs to exactly one tenant, so RLS scopes both reads and the
-- fan-out to the bound tenant (no cross-tenant delivery, no open relay).
create table if not exists suite_webhook_subscriptions (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid not null references suite_tenants(id) on delete cascade,
  -- Destination URL the tenant wants events delivered to.
  url text not null,
  -- Event types this subscription wants; empty array = all events.
  events text[] not null default '{}',
  -- Per-subscription HMAC signing secret (generated on create). Delivered
  -- events carry X-AF-Webhook-Signature = sha256(hmac(secret, body)) so
  -- the subscriber can verify authenticity.
  secret text not null,
  is_active boolean not null default true,
  created_at timestamptz not null default now()
);

create index if not exists suite_webhook_subscriptions_tenant_idx
  on suite_webhook_subscriptions (tenant_id, created_at desc);

-- RLS — same GUC pattern as suite_webhook_endpoints, minus the null-tenant
-- clause (subscriptions are never tenant-less).
alter table suite_webhook_subscriptions enable row level security;
alter table suite_webhook_subscriptions force row level security;

create policy tenant_isolation on suite_webhook_subscriptions
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_webhook_subscriptions;
alter table suite_webhook_subscriptions no force row level security;
alter table suite_webhook_subscriptions disable row level security;
drop table if exists suite_webhook_subscriptions;
