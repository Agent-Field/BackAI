-- +goose Up
-- R8 — minimum complete SaaS lifecycle.
--
-- This migration adds the schema the lifecycle surface needs on top of the
-- primitives that already exist (suite_tenants soft-delete via deleted_at,
-- suite_memberships roles, suite_api_keys expires_at/scopes,
-- suite_billing_customers subscription mirror):
--
--   1. suite_api_keys.service_account_name — labels a key as belonging to a
--      named non-human service account (distinct from the human `name`).
--   2. suite_memberships gains the 'billing' role (billing-only access).
--   3. suite_invitations — token-based membership invitations with an
--      explicit accept/revoke/expire state machine.
--   4. suite_billing_events — idempotency ledger for Stripe webhook +
--      reconciliation ingestion (unique per (tenant_id, event_id)).
--
-- Tenant-owned tables (suite_invitations, suite_billing_events) carry a
-- tenant_id + FORCE ROW LEVEL SECURITY, following 00004_rls.sql /
-- 00032_memory_tenant_rls.sql. The admin/reconciliation paths read
-- cross-tenant via app.bypass_rls=on inside a transaction.

-- ─── 1. Service-account label on API keys ─────────────────────────────────
alter table suite_api_keys
  add column if not exists service_account_name text;

create index if not exists suite_api_keys_service_account_idx
  on suite_api_keys (tenant_id, service_account_name)
  where service_account_name is not null;

-- ─── 2. 'billing' membership role ─────────────────────────────────────────
-- 00001_init.sql created the role CHECK inline (auto-named
-- suite_memberships_role_check). Drop + re-add to widen the allowed set.
alter table suite_memberships
  drop constraint if exists suite_memberships_role_check;
alter table suite_memberships
  add constraint suite_memberships_role_check
  check (role in ('owner','admin','member','billing','viewer'));

-- ─── 3. Invitations ───────────────────────────────────────────────────────
-- token_hash is the sha256 of the one-time invite token; the plaintext is
-- shown once to the inviter and delivered to the invitee out-of-band (email
-- via the notifications subsystem). The token is the accept capability, so
-- the accept lookup runs under app.bypass_rls (the invitee has no tenant
-- membership yet, hence no tenant binding).
create table if not exists suite_invitations (
  id           uuid primary key default gen_random_uuid(),
  tenant_id    uuid not null references suite_tenants(id) on delete cascade,
  email        text not null,
  role         text not null
                 check (role in ('owner','admin','member','billing','viewer')),
  token_hash   text not null,
  status       text not null default 'pending'
                 check (status in ('pending','accepted','revoked','expired')),
  invited_by   uuid references suite_users(id),
  accepted_by  uuid references suite_users(id),
  created_at   timestamptz not null default now(),
  expires_at   timestamptz not null,
  accepted_at  timestamptz,
  revoked_at   timestamptz
);

create index if not exists suite_invitations_tenant_status_idx
  on suite_invitations (tenant_id, status);
create unique index if not exists suite_invitations_token_idx
  on suite_invitations (token_hash);
create index if not exists suite_invitations_email_idx
  on suite_invitations (tenant_id, lower(email));

alter table suite_invitations enable row level security;
alter table suite_invitations force row level security;

create policy tenant_isolation on suite_invitations
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- ─── 4. Billing event idempotency ledger ──────────────────────────────────
-- Webhook + reconciliation ingestion records the Stripe event id here inside
-- the same logical operation. A unique (tenant_id, event_id) makes replays a
-- no-op: the second insert conflicts and the handler skips re-applying.
create table if not exists suite_billing_events (
  id           uuid primary key default gen_random_uuid(),
  tenant_id    uuid not null references suite_tenants(id) on delete cascade,
  event_id     text not null,
  event_type   text not null,
  received_at  timestamptz not null default now(),
  unique (tenant_id, event_id)
);

create index if not exists suite_billing_events_tenant_idx
  on suite_billing_events (tenant_id, received_at desc);

alter table suite_billing_events enable row level security;
alter table suite_billing_events force row level security;

create policy tenant_isolation on suite_billing_events
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_billing_events;
alter table suite_billing_events no force row level security;
alter table suite_billing_events disable row level security;
drop index if exists suite_billing_events_tenant_idx;
drop table if exists suite_billing_events;

drop policy if exists tenant_isolation on suite_invitations;
alter table suite_invitations no force row level security;
alter table suite_invitations disable row level security;
drop index if exists suite_invitations_email_idx;
drop index if exists suite_invitations_token_idx;
drop index if exists suite_invitations_tenant_status_idx;
drop table if exists suite_invitations;

alter table suite_memberships
  drop constraint if exists suite_memberships_role_check;
alter table suite_memberships
  add constraint suite_memberships_role_check
  check (role in ('owner','admin','member','viewer'));

drop index if exists suite_api_keys_service_account_idx;
alter table suite_api_keys
  drop column if exists service_account_name;
