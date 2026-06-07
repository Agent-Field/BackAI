---
title: Module — Memory
description: Scope/key store with optional pgvector similarity search. Powers agent long-term recall.
sidebar:
  order: 4
---

Scope+key store with optional vector similarity search. Scopes are arbitrary strings (`tenant`, `agent`, `thread`, `user`...). Search is gated on whether an embedder is wired.

## What it does

`memory.Store` is a Postgres-backed key/value store with an optional `Embed=true` flag on `Put`. When set, the value is embedded via the configured `memory.Embedder` (typically routed through the LLM Gateway) and stored alongside the row. Search uses pgvector cosine similarity.

Without an embedder, every non-vector operation (`Get`, `Put` w/o embed, `Delete`, `List`) still works; `Search` returns `503 EMBEDDER_NOT_CONFIGURED`.

When no DB is wired, all `/api/v1/memory/*` routes return `503`.

## Configuration

No dedicated flag. The store is constructed whenever a DB pool exists; the embedder is constructed whenever the LLM Gateway has a provider key.

```go
// cmd/af-stack/main.go
if llmGW != nil {
    embedder = memory.NewLLMGatewayEmbedder(llmGW, "")
}
```

Default embedding model: `memory.DefaultEmbeddingModel`.

## REST endpoints

Registered in `services/runtime/internal/server/memory.go`:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/memory` | List memory entries (paginated). Query: `scope`, `scope_id`, `prefix`, `limit`, `offset`. |
| `GET` | `/api/v1/memory/get` | Get a single entry. Query: `scope` (required), `key` (required), `scope_id`. |
| `PUT` | `/api/v1/memory` | Upsert a memory entry. |
| `DELETE` | `/api/v1/memory` | Delete a memory entry. Query: `scope` (required), `key` (required), `scope_id`. |
| `POST` | `/api/v1/memory/search` | Vector-similarity search. Returns `503` when no embedder is wired. |

## Database tables

Owned by migration `00007_memory.sql`:

- `suite_memory` — (tenant, scope, scope_id, key, value JSONB, embedding vector, updated_at).

## Env vars

None directly. Embedding routes through the LLM Gateway's provider keys.

## Code map

- `store.go` — `Store` with `Get`, `Put`, `Delete`, `List`, `Search`.
- `embedder.go` — `Embedder` interface + LLM-gateway-backed implementation.
- `types.go` — wire shapes mirroring `apps/dashboard/src/lib/api.ts`.
- `errors.go` — sentinel errors.

## Related

- Routes embeddings through [LLM Gateway](./llm-gateway/).
- Tenant-scoped via [Multi-tenancy](./multi-tenancy/).
