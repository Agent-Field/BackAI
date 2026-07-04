---
title: Module — Billing
description: Turnkey plans, Stripe auto-provisioning, hosted checkout, entitlements, usage meters, and budget enforcement. Deterministic stub when no Stripe key is set.
sidebar:
  order: 8
---

A complete pricing engine behind a single `billing.Service`: a plan
catalog, Stripe Product/Price **auto-provisioning**, hosted checkout,
per-plan **entitlements**, per-tenant usage meters, and budget enforcement
wired to the LLM gateway.

For the end-to-end setup guide — the agent-first `af-stack billing` flow and
the three app-side calls — see `docs/billing.md` in the repo.

## What it does

Layers:

- **Plans** — a catalog of `{id, name, price, budget, entitlements, default}`.
  Defining a paid plan **auto-provisions** the Stripe Product + Price (or
  binds an existing `stripe_price_id`). The "free" plan is implicit.
- **Entitlements** — typed per-plan limits (`simulations=500`, `seats=5`)
  that app code reads via `GET /api/v1/billing/entitlements` to gate features.
- **Checkout** — `POST /api/v1/billing/checkout` returns a hosted Stripe
  checkout URL; in stub mode the plan applies instantly.
- **Store** — direct SQL against `suite_billing_customers` + `suite_usage_meters`.
- **Client** — thin wrapper around `stripe-go` with a deterministic stub mode for dev (no `STRIPE_SECRET_KEY`).
- **Service** — composes the above, gates budgets, produces dashboard wire shapes.

Wire shapes mirror the zod schemas in `apps/dashboard/src/lib/api.ts` "Billing" section. Nullable fields use pointer types so JSON emits `null`. The "free" plan is implicit and never has a `stripe_customer_id`.

Budgets flow to the LLM gateway: a tenant over its plan's monthly budget gets `402 BUDGET_EXCEEDED` on gateway calls. When no service is wired, reads serve empty / synthesised rows; mutations like the portal link return `503 BILLING_NOT_CONFIGURED`.

Live vs test Stripe keys are tracked: swapping keys reconciles plan prices to the new mode, and `af-stack billing status` surfaces the active key mode so a swap doesn't silently 404 checkout.

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
| `GET` | `/api/v1/billing/plans` | Public plan catalog. |
| `GET` | `/api/v1/billing/entitlements` | Current plan + entitlements + usage (one read to gate features). |
| `POST` | `/api/v1/billing/checkout` | Hosted Stripe checkout URL (instant apply in stub mode). |
| `POST` | `/api/v1/billing/meter` | Record a usage event. |
| `GET` | `/api/v1/billing/customers` | List billing customers. |
| `GET` | `/api/v1/billing/customers/{tenantId}` | Get a single customer. |
| `GET` | `/api/v1/billing/meters` | List per-tenant usage meters. |
| `POST` | `/api/v1/billing/customers/{tenantId}/portal` | Generate a Stripe billing-portal link. |
| `PUT` | `/api/v1/admin/billing/plans` | Create/update a plan (auto-provisions Stripe Product + Price). |
| `DELETE` | `/api/v1/admin/billing/plans/{id}` | Delete a plan. |
| `GET` · `PUT` | `/api/v1/admin/billing/settings` | Read/store the Stripe key + mode. |
| `POST` | `/webhooks/in/stripe` | Stripe webhook receiver (signature verified). |

Registered across `server/billing.go` and `server/billing_plans.go`. The
`admin/billing/*` routes are what the `af-stack billing` CLI and the
dashboard **Platform → Billing** page drive.

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
