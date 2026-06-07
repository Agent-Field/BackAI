-- +goose Up
-- LLM response cache (Phase 7.3).
--
-- Exact-match cache for upstream LLM responses keyed by a stable hash of
-- the canonicalised request body. The Phase 7.1 LLM gateway checks this
-- table before calling upstream and inserts on miss; this migration only
-- creates the storage.
--
-- Tenant scope:
--   `key` is constructed by the application layer as
--   "<tenant_id>:<model>:<request_hash>" so different tenants never
--   collide on the same prompt+model. tenant_id is also stored as a
--   separate column so per-tenant stats and eviction stay efficient.
--   tenant_id is nullable so unauthenticated / system calls (which still
--   benefit from caching) can land in the table.
--
-- Hit accounting:
--   `hits` counts cache hits AFTER the initial Put (the Put itself is a
--   miss-then-store). `cost_saved_usd` is the sum of would-be upstream
--   costs that the cache served instead — computed from pricing.go at
--   Get time and added per hit.
--
-- Expiry & eviction:
--   `expires_at` is the TTL deadline; rows past it are returned as
--   misses and reaped by the background eviction goroutine in
--   internal/llmcache/runner.go.

create table if not exists suite_llm_cache (
  key              text primary key,
  tenant_id        uuid,
  model            text not null,
  request_hash     text not null,
  response_json    jsonb not null,
  prompt_tokens    int not null default 0,
  completion_tokens int not null default 0,
  hits             int not null default 0,
  cost_saved_usd   numeric(12,6) not null default 0,
  created_at       timestamptz not null default now(),
  expires_at       timestamptz not null,
  last_hit_at      timestamptz
);

create index suite_llm_cache_expires_idx
  on suite_llm_cache (expires_at);

create index suite_llm_cache_tenant_model_idx
  on suite_llm_cache (tenant_id, model);

-- +goose Down
drop table if exists suite_llm_cache;
