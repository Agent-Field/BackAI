-- +goose Up
-- Runtime-wide inbound idempotency (PRD R1).
--
-- The gateway middleware stores, per (tenant_id, idempotency_key), the
-- fingerprint of the originating request (method + path + query + body
-- hash) alongside the captured response so a retry with the SAME key can
-- be replayed WITHOUT re-running the handler (no duplicate side effects).
--
-- Row lifecycle:
--   * status_code IS NULL   -> reserved / in-flight (the owning request is
--                              still running). A concurrent duplicate sees
--                              this and gets 409 IDEMPOTENCY_IN_FLIGHT.
--   * status_code NOT NULL   -> completed. A duplicate with the same
--                              fingerprint replays the stored response;
--                              a duplicate with a different fingerprint
--                              gets 422 IDEMPOTENCY_KEY_REUSED.
--
-- expires_at defaults to 24h out. Expired rows are purged opportunistically
-- (the store deletes this tenant's expired rows on each reserve, RLS-scoped)
-- so a key becomes reusable after its TTL.
--
-- Tenant-owned table: tenant_id + FORCE ROW LEVEL SECURITY per the
-- 00004_rls.sql / 00032_memory_tenant_rls.sql pattern. The unique key is
-- (tenant_id, idempotency_key) so one tenant's keys never collide with
-- another's, and RLS keeps them mutually invisible.

create table if not exists suite_idempotency_keys (
  id               uuid primary key default gen_random_uuid(),
  tenant_id        uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  idempotency_key  text not null,
  fingerprint      text not null,
  status_code      int,
  response_headers jsonb,
  response_body    bytea,
  created_at       timestamptz not null default now(),
  completed_at     timestamptz,
  expires_at       timestamptz not null default (now() + interval '24 hours')
);

-- ON CONFLICT (tenant_id, idempotency_key) DO NOTHING in the store relies on
-- this unique index to atomically detect a concurrent claim.
create unique index if not exists suite_idempotency_keys_tenant_key_uidx
  on suite_idempotency_keys (tenant_id, idempotency_key);

-- Supports the opportunistic expired-row purge.
create index if not exists suite_idempotency_keys_expires_idx
  on suite_idempotency_keys (expires_at);

alter table suite_idempotency_keys enable row level security;
alter table suite_idempotency_keys force row level security;

create policy tenant_isolation on suite_idempotency_keys
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_idempotency_keys;
alter table suite_idempotency_keys no force row level security;
alter table suite_idempotency_keys disable row level security;
drop index if exists suite_idempotency_keys_expires_idx;
drop index if exists suite_idempotency_keys_tenant_key_uidx;
drop table if exists suite_idempotency_keys;
