---
title: Module — LLM Gateway
description: OpenAI-compatible proxy for chat, embeddings, and image generation. Fires LLM hooks for cost + budget enforcement.
sidebar:
  order: 2
---

OpenAI-compatible LLM proxy. Routes `/api/v1/llm/*` to a provider client (AgentField built-in or direct OpenAI-compatible upstream). Fires `llm.pre_call` + `llm.post_call` hooks so the cost ledger + budget guard can attach.

## What it does

`llmgateway.Gateway` is provider-agnostic. It delegates the upstream HTTP call to a `ProviderClient`:

- **AFProvider** — routes through an AgentField reasoner (canonical).
- **OpenAIProvider** — direct OpenAI-compatible upstream (OpenRouter / OpenAI / Anthropic).

Pre-call hook can short-circuit (e.g. budget guard returns 402). Post-call hook is fire-and-forget — failures log but don't affect the response.

When no provider key is set the gateway is not wired and `/api/v1/llm/*` returns `503`.

## Configuration

No dedicated module flag — gateway is on whenever at least one provider key is present.

Pick a provider via env:

```bash
# Recommended: one OpenRouter key, many models
OPENROUTER_API_KEY=sk-or-...

# Or direct providers:
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
```

`buildLLMGateway` in `cmd/af-stack/main.go` prefers OpenRouter, then OpenAI, then Anthropic. The gateway logs `(OPENROUTER_API_KEY, OPENAI_API_KEY, ANTHROPIC_API_KEY) unset` when none are present.

## REST endpoints

Registered in `services/runtime/internal/server/llm.go`:

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/llm/chat/completions` | OpenAI-compatible chat completion (sync or SSE stream). |
| `POST` | `/api/v1/llm/embeddings` | OpenAI-compatible embeddings. |
| `POST` | `/api/v1/llm/images/generations` | OpenAI-compatible image generation. |
| `GET` | `/api/v1/llm/models` | List supported models with pricing + capability flags. |
| `GET` | `/api/v1/llm/cache/stats` | LLM response-cache statistics (delegates to [LLM Cache](./llm-cache/)). |

Error envelope uses the OpenAI `{"error":{"message","code","type"}}` shape so the official OpenAI SDK parses it natively.

## Database tables

None directly. The gateway fires hooks consumed by:

- [Cost](./cost/) — writes to `suite_cost_events`.
- [LLM Cache](./llm-cache/) — reads/writes `suite_llm_cache`.

## Env vars

| Env | Purpose |
|---|---|
| `OPENROUTER_API_KEY` | OpenRouter key (preferred). |
| `OPENAI_API_KEY` | OpenAI direct. |
| `ANTHROPIC_API_KEY` | Anthropic direct. |

## Code map

- `gateway.go` — `Gateway` façade + `ProviderClient` interface.
- `af_provider.go` — AgentField reasoner path.
- `openai_provider.go` — OpenAI-compatible HTTP path.
- `chat.go` / `embeddings.go` / `images.go` — operation-specific request/response shapes.
- `streaming.go` — SSE relay.
- `models.go` — model catalogue + pricing snapshot.
- `server/llm.go` — REST routes, `LLMPreCallPayload`, `LLMPostCallPayload`, hook plumbing.

## Related

- Fires [`llm.pre_call`](../../hooks/#llmprecall) + [`llm.post_call`](../../hooks/#llmpostcall).
- Consumed by [Cost](./cost/) and [LLM Cache](./llm-cache/).
