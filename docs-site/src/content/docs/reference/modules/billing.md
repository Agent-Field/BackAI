---
title: Module — Billing
description: Stripe customers + per-tenant usage meters. Deterministic stub when no Stripe key is set.
sidebar:
  order: 8
---

Stripe integration + per-tenant usage meter aggregation behind a single `billing.Service`.

## What it does

Three layers:

- **Store** — direct SQL against `suite_billing_customers` + `suite_usage_meters`.
- **Client** — thin wrapper around `stripe-go` with a deterministic stub mode for dev (no `STRIPE_SECRET_KEY`).
- **Service** — composes Store + Client, gates budgets, produces dashboard wire shapes.

Wire shapes mirror the zod schemas in `apps/dashboard/src/lib/api.ts` "Billing (Phase 10.4)" section. Nullable fields use pointer types so JSON emits `null`. The "free" plan is implicit and never has a `stripe_customer_id`.

When no service is wired, reads serve empty / synthesised rows; the portal-link mutation returns `503 BILLING_NOT_CONFIGURED`.

## Configuration

```yaml
modules:
  enabled:
    billing: true
```

Env:

```bash
AF_STACK_MODULE_BILLING=true
STRIPE_SECRET_KEY=sk_test_...       # absent ⇒ stub mode
STRIPE_WEBHOOK_SECRET=whsec_...     # required for webhook signature verify
```

## REST endpoints

Registered in `services/runtime/internal/server/billing.go`:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/billing/customers` | List billing customers. |
| `GET` | `/api/v1/billing/customers/{tenantId}` | Get a single customer. |
| `GET` | `/api/v1/billing/meters` | List per-tenant usage meters. |
| `POST` | `/api/v1/billing/customers/{tenantId}/portal` | Generate a Stripe billing-portal link. |
| `POST` | `/webhooks/in/stripe` | Stripe webhook receiver (signature verified). |

## Database tables

Owned by migration `00011_billing.sql`:

- `suite_billing_customers` — tenant → Stripe customer id.
- `suite_usage_meters` — per-tenant metered values (events count, period anchors).

## Env vars

| Env | Purpose |
|---|---|
| `AF_STACK_MODULE_BILLING` | Enable / disable. |
| `STRIPE_SECRET_KEY` | Activates real Stripe client. Unset ⇒ stub mode. |
| `STRIPE_WEBHOOK_SECRET` | Signature verification for `/webhooks/in/stripe`. |

Constants are exposed as `billing.EnvSecretKey` and `billing.EnvWebhookSecret` in `stripe_client.go`.

## Code map

- `interface.go` — public types + service shape.
- `store.go` — Postgres queries.
- `stripe_client.go` — real vs stub Client toggle.
- `service.go` — orchestration.
- `webhook_handler.go` — `POST /webhooks/in/stripe`.
- `server/billing.go` — REST routes.

## Related

- Fires [`billing.pre_charge`](../../hooks/#billingprecharge) (scaffolded; not yet wired in v1).
- Tenant-scoped via [Multi-tenancy](./multi-tenancy/).
