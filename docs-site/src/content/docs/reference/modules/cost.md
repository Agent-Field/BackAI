---
title: Module — Cost
description: Per-call LLM cost ledger and per-tenant monthly budget guard.
sidebar:
  order: 10
---

Per-call LLM cost tracking + per-tenant budget enforcement. Hooks into the [LLM Gateway](./llm-gateway/) — pre-call asks `Budgets.HasBudget`; post-call writes one row to `suite_cost_events`.

## What it does

Architecture:

1. The LLM gateway fires `hooks.HookLLMPreCall` before each upstream call and `hooks.HookLLMPostCall` after.
2. **Pre-call** — `Budgets.HasBudget` checks current-period spend vs the tenant's `monthly_usd` cap. Nil budget ⇒ no cap, calls pass. Exhausted budget ⇒ `ErrBudgetExceeded`, gateway translates to HTTP `402`.
3. **Post-call** — `Recorder.Record` writes one row to `suite_cost_events`. Best-effort by design: a write failure logs but never errors the LLM call.

`Aggregate.Summary` powers `/api/v1/cost`; `Aggregate.Events` powers `/api/v1/cost/events`. Forecast is linear extrapolation: `current_period_total / elapsed_fraction`.

When no DB is present, the recorder + budgets are constructed but short-circuit to permissive no-ops.

## Configuration

No dedicated module flag. Hooks register whenever a DB pool exists at boot. Per-tenant caps are set via the admin REST surface (`PUT /api/v1/admin/budgets`).

## REST endpoints

Registered in `services/runtime/internal/server/cost.go`:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/cost/events` | List recorded cost events. |
| `GET` | `/api/v1/admin/budgets` | List per-tenant budgets. |
| `GET` | `/api/v1/admin/budgets/{tenantId}` | Get a single tenant's budget. |
| `PUT` | `/api/v1/admin/budgets` | Set / update a budget. |

Admin endpoints are gated on the [Multi-tenancy](./multi-tenancy/) flag.

## Database tables

Owned by migration `00005_cost.sql`:

- `suite_cost_events` — one row per LLM call (tenant, request_id, model, provider, tokens, cost_usd, latency_ms, occurred_at).
- `suite_budgets` — per-tenant monthly cap.

## Env vars

None directly.

## Code map

- `cost.go` — package doc + shared types.
- `recorder.go` — `Recorder.Record` (writes `suite_cost_events`).
- `budgets.go` — `Budgets.HasBudget` + cap CRUD.
- `aggregate.go` — `Aggregate.Summary` + `Aggregate.Events`.
- `hooks.go` — `PreCallHandler` + `PostCallHandler` constructors (consume `LLMPreCallPayload` / `LLMPostCallPayload`).
- `server/cost.go` — REST routes.

## Related

- Listens on [`llm.pre_call`](../../hooks/#llmprecall) + [`llm.post_call`](../../hooks/#llmpostcall).
- Powered by [LLM Gateway](./llm-gateway/) firings.
- Aggregated with [Billing](./billing/) meter values for invoicing.
