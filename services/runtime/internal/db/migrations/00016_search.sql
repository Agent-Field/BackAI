-- +goose Up
-- Phase 2 — tenant-scoped app-data search index.
--
-- This is intentionally separate from suite_memory and AgentField run /
-- trace state. AF Stack apps and workload modules index their product
-- records here, then query them through /api/v1/search using Postgres
-- full-text search by default and pgvector when an embedder is wired.

create extension if not exists vector with schema public;

create table if not exists suite_search_documents (
  tenant_id uuid not null references suite_tenants(id) on delete cascade
    default '00000000-0000-0000-0000-000000000000',
  namespace text not null default 'default',
  key text not null,
  title text not null default '',
  body text not null default '',
  metadata jsonb not null default '{}'::jsonb,
  embedding public.vector(1536),
  search_vector tsvector generated always as (
    setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
    setweight(to_tsvector('simple', coalesce(body, '')), 'B')
  ) stored,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (tenant_id, namespace, key)
);

create index if not exists suite_search_documents_tenant_ns_idx
  on suite_search_documents (tenant_id, namespace);

create index if not exists suite_search_documents_fts_idx
  on suite_search_documents using gin (search_vector);

create index if not exists suite_search_documents_embedding_idx
  on suite_search_documents using hnsw (embedding public.vector_cosine_ops);

alter table suite_search_documents enable row level security;
alter table suite_search_documents force row level security;

create policy tenant_isolation on suite_search_documents
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );

-- +goose Down
drop policy if exists tenant_isolation on suite_search_documents;
alter table suite_search_documents no force row level security;
alter table suite_search_documents disable row level security;
drop index if exists suite_search_documents_embedding_idx;
drop index if exists suite_search_documents_fts_idx;
drop index if exists suite_search_documents_tenant_ns_idx;
drop table if exists suite_search_documents;
-- intentionally leave vector extension installed
