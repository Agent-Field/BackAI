-- +goose Up
-- Explicit operator allow-list for the dashboard.
--
-- The customer app and operator dashboard share better-auth's "user" table,
-- so counting all auth users is not a valid operator bootstrap signal. This
-- table marks which auth users may enter the operator console.

create table if not exists suite_operators (
  id          uuid primary key default gen_random_uuid(),
  user_id     text unique,
  email       text unique not null,
  name        text,
  role        text not null default 'owner' check (role in ('owner','admin')),
  created_at  timestamptz not null default now()
);

create index if not exists suite_operators_email_idx on suite_operators (email);

-- +goose Down
drop table if exists suite_operators;
