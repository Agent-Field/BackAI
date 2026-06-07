-- +goose Up
-- Phase 8.2 — suite memory store.
--
-- The memory table holds per-scope key/value entries with an optional
-- pgvector embedding for similarity search. Scopes are: global, tenant,
-- agent, session, run. scope_id partitions entries within a scope (the
-- tenant uuid, the agent name, the session id, etc.). Empty string is
-- the canonical "no scope_id" value (used for scope='global') because
-- the primary key cannot include nulls.
--
-- pgvector is shipped with the compose image (pgvector/pgvector:pg16);
-- the extension only needs to be flipped on. text-embedding-3-small
-- emits 1536-dim vectors, which we hardcode here so the HNSW index is
-- valid at create time.
--
-- The `vector` type is fully-qualified as `public.vector` so callers
-- that pin search_path to a single schema (e.g. the migration round-
-- trip test in services/runtime/internal/db) still find the type.

create extension if not exists vector with schema public;

create table if not exists suite_memory (
  scope text not null,
  scope_id text not null default '',
  key text not null,
  value jsonb not null,
  metadata jsonb not null default '{}'::jsonb,
  embedding public.vector(1536),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (scope, scope_id, key)
);
create index if not exists suite_memory_scope_idx on suite_memory (scope, scope_id);
create index if not exists suite_memory_embedding_idx on suite_memory using hnsw (embedding public.vector_cosine_ops);

-- +goose Down
drop table if exists suite_memory;
-- intentionally leave vector extension installed
