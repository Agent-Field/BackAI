-- +goose Up
-- Secrets vault storage.
--
-- One row per (tenant, key). encrypted_value is the AES-256-GCM ciphertext
-- produced by services/runtime/internal/secrets/crypto.go in the format
-- [version_byte | nonce(12) | ciphertext]. kms_key_id records the KEK
-- version used so future rotations can re-encrypt selectively.
--
-- metadata jsonb holds non-secret descriptive fields (description, etc.).
-- The plaintext value never lives in this table or any other.

create table if not exists suite_secrets (
  tenant_id        uuid not null references suite_tenants(id) on delete cascade,
  key              text not null,
  encrypted_value  bytea not null,
  kms_key_id       text not null default 'v1',
  metadata         jsonb not null default '{}'::jsonb,
  rotate_after     timestamptz,
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now(),
  primary key (tenant_id, key)
);

create index if not exists suite_secrets_tenant_key_idx
  on suite_secrets (tenant_id, key);

-- +goose Down
drop table if exists suite_secrets;
