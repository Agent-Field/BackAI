# Billing — turnkey plans, checkout, and entitlements

BackAI ships a complete pricing engine so you don't build one. It gives you
a **plan catalog**, **Stripe auto-provisioning** (Products + Prices created
for you), **hosted checkout**, **entitlements** (per-plan feature limits),
and **hard budget enforcement** wired to the LLM gateway. Your app makes
three calls — gate a feature, record usage, send an upgrade — instead of
owning a billing system.

The setup surface is **agent-first**: an agent can define the whole plan
catalog from the CLI, and the runtime provisions the Stripe objects. The
one thing an agent can't invent is your Stripe secret key.

## Concepts

- **Plan** — a slug (`free`, `pro`, …), a display name, a monthly price in
  USD, an optional enforced **LLM budget** (USD/month), and a set of
  **entitlements**. The `free` plan is implicit and needs no Stripe object.
- **Entitlement** — a typed key→value limit an app reads to gate features,
  e.g. `simulations=500`, `seats=5`. Numbers are stored as numbers.
- **Budget** — a per-tenant monthly LLM spend cap. When exceeded, gateway
  calls return `402 BUDGET_EXCEEDED`. See [`docs/CONFIGURATION.md`](CONFIGURATION.md).
- **Mode** — `stub` (no Stripe key: checkout applies the plan instantly,
  perfect for dev and personal mode) vs `real` (a Stripe key is set: prices
  are provisioned and checkout is hosted by Stripe).

## Set it up with the CLI

All `billing` commands need `AF_STACK_API_KEY` set to an operator key
(`af-stack operator key`).

```bash
# 1. See where you stand — adapter, stub vs real, and the next step.
af-stack billing status

# 2. (real mode only) Store your Stripe secret key once. In stub/dev you can
#    skip this entirely.
af-stack billing set-key --stripe-secret sk_test_… [--stripe-webhook whsec_…]

# 3. Define the catalog. In real mode this auto-provisions the Stripe
#    Product + Price; in stub mode it just records the plan.
af-stack billing plan set --id free --name Free  --price 0 --entitlement simulations=20
af-stack billing plan set --id pro  --name Pro   --price 29 --budget 25 \
        --entitlement simulations=500 --entitlement seats=5 --default

# 4. Inspect / remove.
af-stack billing plans
af-stack billing plan rm pro
```

`plan set` flags: `--id` (slug, required), `--name` (required), `--price`
(USD/month; `0` = free, `>0` provisions a Stripe Price), `--budget`
(enforced LLM USD/month; omit for none), `--entitlement k=v` (repeatable),
`--stripe-price price_…` (bind an existing Stripe Price instead of
auto-provisioning), `--default` (make this the fallback plan).

> **Stripe live vs test keys.** Prices provisioned under one key's mode
> (`live`/`test`) return 404 at checkout under the other. `af-stack billing
> status` prints the current key mode so a key swap doesn't silently break
> checkout; swapping keys reconciles plan prices to the new mode.

## Use it from your app — three calls

Your app code should never hardcode prices or plan logic. Read entitlements,
meter usage, and hand off upgrades to hosted checkout.

```bash
# Gate a paid feature: plan + entitlements + current usage in one read.
GET  /api/v1/billing/entitlements

# Record a usage event (tenant inferred from the API key/session).
POST /api/v1/billing/meter        { "event": "simulation", "value": 1 }

# Send the user to upgrade. Returns a checkout URL; in stub mode the plan
# applies instantly.
POST /api/v1/billing/checkout     { "plan": "pro" }
```

Typed SDK equivalents exist under `suite.billing` (TypeScript and Python) —
`suite.billing.entitlements()`, `suite.billing.meter(...)`,
`suite.billing.checkout(...)`.

## REST surface

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/billing/plans` | tenant | The public plan catalog. |
| `GET` | `/api/v1/billing/entitlements` | tenant | Current plan + entitlements + usage. |
| `POST` | `/api/v1/billing/checkout` | tenant | Hosted checkout URL (instant apply in stub mode). |
| `POST` | `/api/v1/billing/meter` | tenant | Record a usage event. |
| `POST` | `/api/v1/billing/customers/{tenantId}/portal` | tenant | Stripe billing-portal link. |
| `GET` | `/api/v1/billing/customers` · `/customers/{tenantId}` · `/meters` | operator | Customer + meter reads for the console. |
| `PUT` | `/api/v1/admin/billing/plans` | operator | Create/update a plan (auto-provisions Stripe objects). |
| `DELETE` | `/api/v1/admin/billing/plans/{id}` | operator | Delete a plan. |
| `GET` · `PUT` | `/api/v1/admin/billing/settings` | operator | Read/store the Stripe key + mode. |
| `POST` | `/webhooks/in/stripe` | signature | Stripe webhook receiver; subscription events update the tenant's plan + budget. |

The `af-stack billing` commands are thin wrappers over the `admin/billing/*`
routes; the dashboard's **Platform → Billing** page drives the same surface
for humans.

## In personal mode

`AF_STACK_MODE=personal` (see [`docs/CONFIGURATION.md`](CONFIGURATION.md))
turns billing **off** — no paywall, no Stripe needed. The billing endpoints
still answer (stub mode), so app code that reads entitlements keeps working;
everyone is effectively on the default plan.

## Where it lives

- Runtime: `services/runtime/internal/billing/` (plans, service, Stripe
  client) and `services/runtime/internal/server/billing*.go` (routes).
- CLI: `services/cli/internal/billingcmd/`.
- Dashboard: **Platform → Billing**.
- Tables: `suite_billing_customers`, `suite_usage_meters`, plus the plan
  catalog (migration under `services/runtime/internal/db/migrations/`).
