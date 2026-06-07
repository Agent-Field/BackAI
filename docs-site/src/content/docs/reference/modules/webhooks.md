---
title: Module — Webhooks
description: Inbound + outbound webhook delivery with HMAC verify and dedup.
sidebar:
  order: 7
---

Inbound + outbound webhook delivery sharing a single `suite_webhook_deliveries` table (column `direction` distinguishes).

## What it does

Two surfaces:

- **Inbound** — endpoints declared via `gateway.yaml` or the SDK. The runtime stores raw payloads, verifies HMAC, dedups by token, forwards to a handler.
- **Outbound** — `webhooks.send()` enqueues a delivery row in the PG outbox. A background worker drains with retries and records the upstream response.

Status lifecycle: `queued → sending → sent | failed` (with retry-eligible failures going back to `queued`).

Wire shapes (`WebhookDeliverySchema`, `WebhookDirectionSchema`, `SendWebhookInputSchema`) live in `apps/dashboard/src/lib/api.ts`.

When no store is wired, mutating endpoints return `503`; reads return empty pages.

## Configuration

```yaml
modules:
  enabled:
    webhooks: true
```

Env override:

```bash
AF_STACK_MODULE_WEBHOOKS=true
```

## REST endpoints

Registered in `services/runtime/internal/server/webhooks.go`:

| Method | Path | Description |
|---|---|---|
| `POST` | `/webhooks/in/{slug}` | Inbound webhook receiver (HMAC-verified, dedup). |
| `GET` | `/api/v1/webhooks/endpoints` | List endpoints. |
| `POST` | `/api/v1/webhooks/endpoints` | Create endpoint. |
| `DELETE` | `/api/v1/webhooks/endpoints/{id}` | Delete endpoint. |
| `POST` | `/api/v1/webhooks/send` | Enqueue an outbound delivery. |
| `GET` | `/api/v1/webhooks/deliveries` | List deliveries. |
| `GET` | `/api/v1/webhooks/deliveries/{id}` | Get a single delivery. |
| `POST` | `/api/v1/webhooks/deliveries/{id}/retry` | Re-queue a failed delivery. |

The Stripe inbound webhook (`POST /webhooks/in/stripe`) is owned by the [Billing](./billing/) module — registered alongside this layer.

## Database tables

Owned by migration `00010_webhooks.sql`:

- `suite_webhook_endpoints` — endpoint definitions (slug, secret, target URL).
- `suite_webhook_deliveries` — every inbound + outbound delivery row.

## Env vars

| Env | Purpose |
|---|---|
| `AF_STACK_MODULE_WEBHOOKS` | Enable / disable. |

## Code map

- `interface.go` — service shape + wire types.
- `endpoints.go` — endpoint CRUD.
- `inbound.go` / `verify.go` / `dedup.go` — HMAC verification + token dedup.
- `outbound.go` / `worker.go` — outbox drain + retries.
- `deliveries.go` — list/get queries.
- `service.go` — façade.
- `util.go` — helpers.
- `server/webhooks.go` — REST routes.

## Related

- [Billing](./billing/) reuses the `/webhooks/in/{slug}` mount point for Stripe.
