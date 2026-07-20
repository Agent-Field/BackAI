-- +goose Up
-- R5 — one secure connection contract for external services.
--
-- suite_connections holds one row per (tenant, external-service connection).
-- Credentials are NEVER stored in plaintext: encrypted_credentials is the
-- AES-256-GCM envelope produced by services/runtime/internal/secrets/crypto.go
-- (the same [version_byte | nonce(12) | ciphertext] format that backs
-- suite_secrets), sealing a small JSON credential blob (an API key, or an
-- OAuth access+refresh token pair). A row leak exposes provider / scopes /
-- status / expiry — never a credential an attacker can authenticate with.
-- kms_key_id records the KEK version used, mirroring suite_secrets.
--
-- webhook_secret_ref points at a suite_secrets entry (never the secret
-- itself) so inbound-webhook signature verification can load the signing
-- secret from the vault, matching the oauth_tokens defense-in-depth pattern.
--
-- Tenant-owned => tenant_id + FORCE ROW LEVEL SECURITY, mirroring
-- 00032_memory_tenant_rls.sql. The tenant boundary is enforced at the
-- database independent of any application logic; app.bypass_rls stays the
-- single operator escape hatch.

create table if not exists suite_connections (
    id                    uuid primary key default gen_random_uuid(),
    tenant_id             uuid not null references suite_tenants(id) on delete cascade,
    provider              text not null,
    kind                  text not null check (kind in ('oauth', 'api_key')),
    name                  text not null default '',
    encrypted_credentials bytea,
    kms_key_id            text not null default 'v1',
    granted_scopes        text[] not null default array[]::text[],
    requested_scopes      text[] not null default array[]::text[],
    status                text not null default 'active'
                              check (status in ('active', 'revoked', 'error')),
    token_expiry          timestamptz,
    webhook_secret_ref    text,
    created_by            text,
    created_at            timestamptz not null default now(),
    updated_at            timestamptz not null default now()
);

create index if not exists suite_connections_tenant_idx
    on suite_connections (tenant_id);
create index if not exists suite_connections_tenant_provider_idx
    on suite_connections (tenant_id, provider);

alter table suite_connections enable row level security;
alter table suite_connections force row level security;

drop policy if exists suite_connections_isolation on suite_connections;
create policy suite_connections_isolation on suite_connections
    using (
        current_setting('app.bypass_rls', true) = 'on'
        or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
    )
    with check (
        current_setting('app.bypass_rls', true) = 'on'
        or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
    );

-- Append-only audit trail: one row per lifecycle event. Distinct from the
-- global suite_audit_log so per-connection history (created / refreshed /
-- revoked / health_check / error) is queryable without scanning the audit
-- firehose.
create table if not exists suite_connection_events (
    id            uuid primary key default gen_random_uuid(),
    tenant_id     uuid not null references suite_tenants(id) on delete cascade,
    connection_id uuid not null references suite_connections(id) on delete cascade,
    event_type    text not null
                      check (event_type in ('created', 'refreshed', 'revoked', 'health_check', 'error')),
    metadata      jsonb not null default '{}'::jsonb,
    occurred_at   timestamptz not null default now()
);

create index if not exists suite_connection_events_conn_idx
    on suite_connection_events (tenant_id, connection_id, occurred_at desc);

alter table suite_connection_events enable row level security;
alter table suite_connection_events force row level security;

drop policy if exists suite_connection_events_isolation on suite_connection_events;
create policy suite_connection_events_isolation on suite_connection_events
    using (
        current_setting('app.bypass_rls', true) = 'on'
        or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
    )
    with check (
        current_setting('app.bypass_rls', true) = 'on'
        or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
    );

-- +goose Down
drop policy if exists suite_connection_events_isolation on suite_connection_events;
alter table suite_connection_events no force row level security;
alter table suite_connection_events disable row level security;
drop table if exists suite_connection_events;

drop policy if exists suite_connections_isolation on suite_connections;
alter table suite_connections no force row level security;
alter table suite_connections disable row level security;
drop table if exists suite_connections;
