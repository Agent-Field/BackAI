# Billing Adapter — Protocol v1

> Inherits from [`PROTOCOL.md`](../PROTOCOL.md).
>
> **Slot:** `billing` · **Base path:** `/v1` · **Go interface:**
> `services/runtime/internal/billing/Client`

## Purpose

Billing adapters create customers in an external billing system, mint
customer-portal links, and verify upstream webhook signatures. Built-in
adapters: `stripe`, `lago`, both with `none` (no-op) and stub modes.

The runtime owns the per-tenant usage ledger; the adapter is the bridge
to the external system that turns usage into invoices.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/customers` | Create an external customer record |
| `GET` | `/v1/customers/{id}` | Fetch a customer |
| `POST` | `/v1/customers/{id}/portal` | Mint a customer-portal URL |
| `POST` | `/v1/webhooks/verify` | Verify an inbound webhook's signature |
| `POST` | `/v1/usage` | (optional) Report usage to the upstream |
| `GET` | `/v1/capabilities` | Capability declaration |
| `GET` | `/healthz` | Liveness |
| `GET` | `/v1/info` | Optional metadata |

## 1. `POST /v1/customers`

Create a customer in the upstream billing system. Returns the upstream
id, which the runtime persists in `suite_billing_customers`.

**Request body**:

```json
{
  "tenant_id": "acme",
  "email": "ops@acme.com",
  "name": "Acme Inc.",
  "metadata": {
    "plan": "free",
    "source": "self-signup"
  }
}
```

| Field | Required | Notes |
|---|---|---|
| `tenant_id` | yes | BackAI tenant id. Adapter persists this as `external_id` / metadata. |
| `email` | yes | Billing email. |
| `name` | optional | Display name. |
| `metadata` | optional | Forwarded to the provider as customer metadata. |

**Response (200 OK)**:

```json
{
  "id": "cus_abc123",
  "tenant_id": "acme",
  "email": "ops@acme.com",
  "plan": "free",
  "trial_ends_at": null,
  "current_period_end": null,
  "subscription_status": null,
  "created_at": "2026-06-15T10:00:00Z",
  "updated_at": "2026-06-15T10:00:00Z"
}
```

| Field | Notes |
|---|---|
| `id` | Upstream provider's customer id (`cus_*` on Stripe, the customer key on Lago). |
| `plan` | Current plan slug. Defaults to `free` for adapters that don't track plans synchronously. |
| `trial_ends_at`, `current_period_end`, `subscription_status` | Nullable. Populated once a subscription exists. |

Idempotency: re-`POST` with the same `X-BackAI-Idempotency-Key` returns
the existing customer.

## 2. `GET /v1/customers/{id}`

Fetch a customer by external id.

**Response (200 OK)**: same shape as above.

**404** with `code: "customer_not_found"`.

## 3. `POST /v1/customers/{id}/portal`

Mint a time-limited customer-portal URL.

**Request body**:

```json
{
  "return_url": "https://acme.com/billing"
}
```

**Response (200 OK)**:

```json
{
  "url": "https://billing.stripe.com/session/...",
  "expires_at": "2026-06-15T11:00:00Z"
}
```

Stub adapters return a deterministic placeholder URL so the dashboard
can render the button.

## 4. `POST /v1/webhooks/verify`

Verify an inbound webhook from the billing provider (e.g., Stripe
`checkout.session.completed`). The runtime exposes
`POST /webhooks/in/stripe` as an inbound endpoint; that handler calls
this adapter endpoint to verify the signature and decode the event.

**Request body**:

```json
{
  "body_base64": "eyJlb...",
  "signature_header": "t=1700000000,v1=abc..."
}
```

`body_base64` is the raw upstream request body, base64-encoded (to avoid
re-encoding issues).

**Response (200 OK)**:

```json
{
  "verified": true,
  "event_type": "checkout.session.completed",
  "event_id": "evt_abc123",
  "decoded": {
    "...": "the parsed event object"
  }
}
```

**400** with `code: "invalid_signature"` if verification fails.

## 5. `POST /v1/usage` (optional)

Push a usage event to the upstream billing system. Only needed for
adapters that support metered billing (Lago, Stripe Meters).

**Request body**:

```json
{
  "customer_id": "cus_abc123",
  "meter": "tokens_in",
  "quantity": 4096,
  "timestamp": "2026-06-15T10:00:00Z",
  "metadata": {
    "model": "openai/gpt-4o"
  }
}
```

**Response (200 OK)**:

```json
{"accepted": true, "external_id": "ue_xyz"}
```

If `capabilities.supports_usage_reporting` is `false` the runtime
doesn't call this and uses its own ledger only.

## 6. `GET /v1/capabilities`

```json
{
  "name": "stripe",
  "version": "1.0.0",
  "slot": "billing",
  "protocol_version": "v1",
  "vendor": "BackAI",
  "capabilities": {
    "supports_customers": true,
    "supports_subscriptions": true,
    "supports_metered_billing": true,
    "supports_customer_portal": true,
    "supports_usage_reporting": true,
    "supports_webhook_verification": true,
    "default_currency": "USD",
    "is_stub": false,
    "admin_dashboard_url": "https://dashboard.stripe.com"
  }
}
```

| Key | Type | Meaning |
|---|---|---|
| `supports_customers` | bool | Whether `/v1/customers/*` is implemented. |
| `supports_subscriptions` | bool | Whether subscription fields in customer responses are meaningful. |
| `supports_metered_billing` | bool | Whether metered usage can be sent. |
| `supports_customer_portal` | bool | Whether `/portal` returns a real URL. |
| `supports_usage_reporting` | bool | Whether `/v1/usage` works. |
| `supports_webhook_verification` | bool | Whether `/v1/webhooks/verify` works. |
| `default_currency` | string | ISO 4217 code. |
| `is_stub` | bool | Whether this adapter is in stub / dev mode. |
| `admin_dashboard_url` | string | Where to link the operator (e.g., dashboard.stripe.com). |

## 7. Error codes

| Code | HTTP | Meaning |
|---|---|---|
| `customer_not_found` | 404 | Unknown customer id. |
| `invalid_signature` | 400 | Webhook signature verification failed. |
| `usage_unsupported` | 422 | `/v1/usage` called on an adapter that doesn't support it. |
| `portal_unsupported` | 422 | Adapter has no portal concept. |
| `provider_error` | 502 | Upstream returned an error. |
| `provider_unavailable` | 503 | Upstream unreachable. |
| `unauthorized` | 401 | Bearer token rejected. |
| `internal_error` | 500 | Catch-all. |

## 8. What's out of scope for v1

The following billing concerns are intentionally NOT in this protocol:

- **Programmatic subscription changes.** Plan upgrades/downgrades are
  portal-driven (`POST /v1/customers/{id}/portal`); the operator hands
  the customer a portal URL and Stripe/Lago/etc. handles the change UI.
- **Refunds and disputes.** Both happen in the upstream provider's own
  dashboard. The runtime never originates a refund.
- **Multi-currency, tax calculation, coupons, batch usage.** Adapter-
  internal concerns; the protocol passes the request to the upstream
  via the portal and customer record.
- **Provider event-type mapping.** When a Stripe `checkout.session.completed`
  webhook arrives, the runtime's inbound webhook handler — not the
  adapter — decides which BackAI domain event fires. The adapter
  verifies the signature and decodes; the rest is the runtime's job.

## 9. Behavior notes

- **Stub mode.** Adapters MAY support a deterministic stub mode for
  local dev (set via env vars before adapter start). In stub mode,
  `capabilities.is_stub` MUST be `true` so the dashboard can warn
  operators "you're in stub billing — no real charges happen."
- **Idempotency for customer creation.** Re-`POST` with the same
  `X-BackAI-Idempotency-Key` MUST return the existing customer rather
  than creating a duplicate. Adapters that wrap providers without
  native idempotency MUST keep their own dedup map.
- **Webhook verification.** Adapters MUST validate the upstream
  signature header against the request body. Failure returns `400`
  rather than `200 + verified: false`, so the runtime can short-circuit
  the inbound webhook handler.

## 10. Mapping back to the Go interface

| Go method | HTTP call |
|---|---|
| `AdapterName()` | cached from `GET /v1/capabilities` → `name` |
| `IsStub()` | cached from `GET /v1/capabilities` → `is_stub` |
| `CreateCustomer(ctx, tenantID, email)` | `POST /v1/customers` |
| `GetCustomer(ctx, id)` | `GET /v1/customers/{id}` |
| `CreatePortalLink(ctx, customerID, returnURL)` | `POST /v1/customers/{id}/portal` |
| `VerifyWebhook(body, sigHeader)` | `POST /v1/webhooks/verify` |

## 11. Conformance checklist

- [ ] `POST /v1/customers` creates a customer and returns an id
- [ ] `GET /v1/customers/{id}` returns the same record
- [ ] `POST /v1/customers/{id}/portal` returns a URL with `expires_at` in the future
- [ ] `POST /v1/webhooks/verify` accepts a valid signature + body and returns `verified=true`
- [ ] `POST /v1/webhooks/verify` rejects a tampered body with `400 + invalid_signature`
- [ ] If `is_stub=true`, the dashboard receives the flag and warns operators
- [ ] Idempotent `POST /v1/customers` (same key) returns the same id, no duplicate created
- [ ] Bearer auth enforced
