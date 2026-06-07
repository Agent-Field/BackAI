---
title: Module — LLM Cache
description: Postgres-backed cache keyed by a deterministic request hash. Replays byte-identical responses on hit.
sidebar:
  order: 3
---

Postgres-backed response cache. Keyed by a deterministic hash of the model + messages + critical params. Cached replies are byte-identical to a fresh call (modulo cache headers).

## What it does

The cache wraps the LLM gateway so chat / embedding responses can be replayed on hit. `Get` returns `Entry{ResponseJSON, ...}` or `ErrNotFound`; the gateway emits the bytes verbatim. `Put` writes the row. The hash includes everything that materially affects the upstream answer (model, messages, temperature, top_p, response_format, ...) so two semantically-identical requests collide.

When no DB is wired, the gateway logs `llmcache not configured (no database); LLM gateway will not cache` and skips the cache path entirely.

## Configuration

No dedicated flag. The cache turns on whenever a DB pool is available at boot.

## REST endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/llm/cache/stats` | Total calls, cache hits/misses, hit rate, savings (USD), entry count. |

Owned by [LLM Gateway](./llm-gateway/); the route lives in `server/llm.go`.

## Database tables

Owned by migration `00006_llmcache.sql`:

- `suite_llm_cache` — one row per cached response (hash, model, response JSON, token counts, cost, created_at).

## Env vars

None — operates off the shared `AF_STACK_DATABASE_URL` pool.

## Code map

- `cache.go` — `Entry`, `Get`, `Put`, `Stats`.
- `hash.go` — deterministic request hashing.
- `runner.go` — wrap-with-cache helper used by the gateway.

## Related

- Consumed by [LLM Gateway](./llm-gateway/).
- Pairs with [Cost](./cost/) for savings reporting.
