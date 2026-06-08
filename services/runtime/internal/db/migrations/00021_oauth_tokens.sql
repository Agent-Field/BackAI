-- +goose Up
-- Phase 2 — OAuth-on-behalf-of-user.
--
-- Stores REFERENCES to tokens stored in the secrets vault, never the token
-- material itself. Defense in depth: a row leak exposes provider, scopes,
-- and expiry, but does NOT expose a token an attacker can authenticate
-- with. The vault keys follow the convention:
--
--   oauth_<provider>_<user_id>          -> access token
--   oauth_<provider>_<user_id>_refresh  -> refresh token
--
-- One row per (tenant_id, user_id, provider). Soft-deleted via revoked_at
-- so disconnects retain an audit trail. Disconnect clears the vault keys
-- and best-effort revokes upstream.
--
-- This table is distinct from better-auth's account/session storage. That
-- handles operator/customer LOGIN OAuth. This table handles AGENT acting
-- AS the user in third-party APIs — different scopes, different tokens,
-- different storage.

create table if not exists suite_oauth_tokens (
    id                uuid primary key default gen_random_uuid(),
    tenant_id         uuid not null references suite_tenants(id) on delete cascade,
    user_id           uuid not null references suite_users(id) on delete cascade,
    provider          text not null check (provider in ('google','github','notion','slack','linear')),
    access_token_ref  text not null,
    refresh_token_ref text,
    scopes            text[] not null default ARRAY[]::text[],
    expires_at        timestamptz,
    created_at        timestamptz not null default now(),
    updated_at        timestamptz not null default now(),
    revoked_at        timestamptz,
    unique (tenant_id, user_id, provider)
);

create index if not exists suite_oauth_tokens_tenant_user_idx
    on suite_oauth_tokens (tenant_id, user_id)
    where revoked_at is null;

create index if not exists suite_oauth_tokens_provider_idx
    on suite_oauth_tokens (provider)
    where revoked_at is null;

alter table suite_oauth_tokens enable row level security;
alter table suite_oauth_tokens force row level security;

drop policy if exists suite_oauth_tokens_isolation on suite_oauth_tokens;
create policy suite_oauth_tokens_isolation on suite_oauth_tokens
    using (
        current_setting('app.bypass_rls', true) = 'on'
        or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
    )
    with check (
        current_setting('app.bypass_rls', true) = 'on'
        or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
    );

-- +goose Down
drop policy if exists suite_oauth_tokens_isolation on suite_oauth_tokens;
alter table suite_oauth_tokens no force row level security;
alter table suite_oauth_tokens disable row level security;
drop index if exists suite_oauth_tokens_provider_idx;
drop index if exists suite_oauth_tokens_tenant_user_idx;
drop table if exists suite_oauth_tokens;
