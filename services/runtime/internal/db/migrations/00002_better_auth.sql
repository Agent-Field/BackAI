-- +goose Up
-- better-auth schema for the operator dashboard.
--
-- The dashboard uses better-auth (https://better-auth.com) to handle login,
-- magic links, OAuth, and sessions. better-auth manages these four tables
-- itself; the suite never writes to them directly. Operator data is mirrored
-- into suite_users by the dashboard so the runtime sees it too.
--
-- IMPORTANT: better-auth defaults to camelCase column names. We use quoted
-- identifiers to preserve case under Postgres' default folding behaviour.

create table if not exists "user" (
  "id"            text primary key,
  "name"          text,
  "email"         text unique not null,
  "emailVerified" boolean not null default false,
  "image"         text,
  "createdAt"     timestamptz not null default now(),
  "updatedAt"     timestamptz not null default now()
);

create index if not exists "user_email_idx" on "user" ("email");

create table if not exists "session" (
  "id"         text primary key,
  "expiresAt"  timestamptz not null,
  "token"      text unique not null,
  "createdAt"  timestamptz not null default now(),
  "updatedAt"  timestamptz not null default now(),
  "ipAddress"  text,
  "userAgent"  text,
  "userId"     text not null references "user"("id") on delete cascade
);

create index if not exists "session_user_idx" on "session" ("userId");
create index if not exists "session_token_idx" on "session" ("token");

create table if not exists "account" (
  "id"                      text primary key,
  "accountId"               text not null,
  "providerId"              text not null,
  "userId"                  text not null references "user"("id") on delete cascade,
  "accessToken"             text,
  "refreshToken"            text,
  "idToken"                 text,
  "accessTokenExpiresAt"    timestamptz,
  "refreshTokenExpiresAt"   timestamptz,
  "scope"                   text,
  "password"                text,
  "createdAt"               timestamptz not null default now(),
  "updatedAt"               timestamptz not null default now()
);

create index if not exists "account_user_idx" on "account" ("userId");
create unique index if not exists "account_provider_account_idx"
  on "account" ("providerId", "accountId");

create table if not exists "verification" (
  "id"          text primary key,
  "identifier"  text not null,
  "value"       text not null,
  "expiresAt"   timestamptz not null,
  "createdAt"   timestamptz not null default now(),
  "updatedAt"   timestamptz not null default now()
);

create index if not exists "verification_identifier_idx" on "verification" ("identifier");

-- +goose Down
drop table if exists "verification";
drop table if exists "account";
drop table if exists "session";
drop table if exists "user";
