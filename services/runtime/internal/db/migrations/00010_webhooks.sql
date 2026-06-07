-- +goose Up
-- Phase 10.2 + 10.3 — suite webhooks (inbound + outbound).
--
-- Two tables back the dashboard's unified webhook surface:
--
--   suite_webhook_endpoints  — INBOUND configuration. One row per
--     declared endpoint. Slug is the public URL component
--     /webhooks/in/<slug>. Globally unique because the slug routes the
--     request before any tenant context exists.
--
--   suite_webhook_deliveries — UNIFIED delivery log. One row per
--     verified inbound request OR per outbound dispatch attempt. The
--     `direction` column distinguishes them so a single list view can
--     surface both with a filter. Phase 10.2 owns the inbound writes
--     (and the row shape); Phase 10.3 owns the outbound worker and
--     reads/writes the same table with direction='outbound'.
--
-- Dedup:
-- (endpoint_id, dedup_token) is unique WHERE dedup_token is not null.
-- The inbound service computes a deterministic token from the request
-- (a provider-specific header like X-GitHub-Delivery, else a sha256 of
-- the body) and lets the insert fail to detect duplicates. The
-- partial-unique guard lets outbound rows omit the token without
-- colliding on (endpoint_id=null, dedup_token=null).
--
-- RLS:
-- Both tables follow the same per-tenant policy as suite_secrets and
-- suite_sandbox_runs (FORCE row level security, USING + WITH CHECK on
-- the GUC app.tenant_id, with an admin bypass via app.bypass_rls).

create table if not exists suite_webhook_endpoints (
  id uuid primary key default gen_random_uuid(),
  slug text unique not null,
  tenant_id uuid references suite_tenants(id) on delete set null,
  provider text not null default 'custom',
  -- Reference to the secret key in suite_secrets (NOT the raw secret).
  -- Nullable: endpoints with no HMAC verification leave it null.
  secret_key_ref text,
  signature_algorithm text,
  signature_header text,
  -- Forward target: "http(s)://..." or "af://agents/<ns.func>".
  forward_to text not null,
  dedup_window_s int not null default 86400,
  is_active boolean not null default true,
  created_at timestamptz not null default now()
);

create table if not exists suite_webhook_deliveries (
  id uuid primary key default gen_random_uuid(),
  endpoint_id uuid references suite_webhook_endpoints(id) on delete set null,
  tenant_id uuid references suite_tenants(id) on delete set null,
  direction text not null check (direction in ('inbound','outbound')),
  destination text not null default '',
  event_type text not null default '',
  status text not null,
  attempts int not null default 0,
  response_status int,
  response_ms int,
  -- body is the raw request payload (inbound: provider POST; outbound:
  -- caller-supplied bytes). Phase 10.3's worker needs it to re-POST on
  -- retry. Capped at ~4 MiB at the HTTP boundary.
  body bytea,
  body_preview text,
  body_storage_url text,
  -- headers carries arbitrary HTTP headers as a JSON map. Outbound
  -- uses it to forward caller-supplied headers; inbound stores the
  -- subset of incoming headers we want to surface in the dashboard.
  headers jsonb not null default '{}'::jsonb,
  last_error text,
  -- Per-endpoint dedup token. Partial-unique index below enforces
  -- (endpoint_id, dedup_token) uniqueness only when token is non-null.
  dedup_token text,
  scheduled_at timestamptz not null default now(),
  delivered_at timestamptz,
  created_at timestamptz not null default now()
);

-- Partial-unique dedup guard (only enforced when token is non-null).
create unique index if not exists suite_webhook_deliveries_dedup_idx
  on suite_webhook_deliveries (endpoint_id, dedup_token)
  where dedup_token is not null;

-- Dashboard query indexes.
create index if not exists suite_webhook_deliveries_direction_status_idx
  on suite_webhook_deliveries (direction, status, scheduled_at);
create index if not exists suite_webhook_deliveries_endpoint_time_idx
  on suite_webhook_deliveries (endpoint_id, created_at desc);
create index if not exists suite_webhook_deliveries_tenant_time_idx
  on suite_webhook_deliveries (tenant_id, created_at desc);

-- RLS — same pattern as suite_secrets (migration 00004).
alter table suite_webhook_endpoints enable row level security;
alter table suite_webhook_endpoints force row level security;

create policy tenant_isolation on suite_webhook_endpoints
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id is null
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id is null
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

alter table suite_webhook_deliveries enable row level security;
alter table suite_webhook_deliveries force row level security;

create policy tenant_isolation on suite_webhook_deliveries
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id is null
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id is null
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
-- Drop in reverse dep order: deliveries -> endpoints (FK).
drop policy if exists tenant_isolation on suite_webhook_deliveries;
alter table suite_webhook_deliveries no force row level security;
alter table suite_webhook_deliveries disable row level security;

drop policy if exists tenant_isolation on suite_webhook_endpoints;
alter table suite_webhook_endpoints no force row level security;
alter table suite_webhook_endpoints disable row level security;

drop table if exists suite_webhook_deliveries;
drop table if exists suite_webhook_endpoints;
