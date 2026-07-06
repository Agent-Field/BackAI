# Webhooks

There are **three** distinct webhook surfaces. They get confused often, so
here they are disambiguated. There is **no Svix** — all delivery is
in-process, native to the runtime.

| # | Surface | Direction | Entry point |
| --- | --- | --- | --- |
| 1 | [Inbound receiver](#1-inbound-receiver) | External → you | `POST /webhooks/in/{slug}` |
| 2 | [Outbound outbox](#2-outbound-outbox) | You → external URL | `suite.webhooks.send` |
| 3 | [Tenant pub-sub](#3-tenant-pubsub) | Tenant ↔ tenant | `/api/v1/webhooks/emit` |

---

## 1. Inbound receiver

A public endpoint that catches webhooks *from* third parties (Stripe,
Shopify, GitHub, …) and routes them into your system.

**`POST /webhooks/in/{slug}`** — for each registered endpoint the runtime:

1. **Verifies the HMAC signature** against the endpoint's secret
   (constant-time compare).
2. **Deduplicates** repeated deliveries.
3. **Forwards** the payload to the endpoint's `forward_to`, which is
   either an `http(s)://…` URL **or** `af://agents/<name>` to hand it
   straight to an agent reasoner.

Manage endpoints via operator-guarded CRUD:

```
GET    /api/v1/webhooks/endpoints
POST   /api/v1/webhooks/endpoints        # { slug, secret, forward_to, … }
DELETE /api/v1/webhooks/endpoints/{id}
```

This surface is public (bypasses the tenant resolver) by design — the HMAC
check is the auth.

---

## 2. Outbound outbox

Send a signed webhook *to* an external URL, with delivery durability
handled by a **native in-process outbox** — HMAC signing, exponential
backoff, automatic retry, and a delivery ledger you can inspect. (Again:
no Svix, no external delivery service.)

```python
from af_stack import suite

await suite.webhooks.send(url="https://acme.example/hook",
                          event_type="order.created",
                          body={"id": "ord_123"})
await suite.webhooks.list()          # the delivery ledger
await suite.webhooks.get(id)
await suite.webhooks.retry(id)       # re-deliver a failed one
```

| SDK method | REST |
| --- | --- |
| `suite.webhooks.send` | `POST /api/v1/webhooks/send` |
| `suite.webhooks.list` | `GET /api/v1/webhooks/deliveries` |
| `suite.webhooks.get` | `GET /api/v1/webhooks/deliveries/{id}` |
| `suite.webhooks.retry` | `POST /api/v1/webhooks/deliveries/{id}/retry` |

`send` accepts `url`, `event_type`, `body`, optional `headers`, and
`immediate`. These methods exist in **both** the Python and TypeScript
SDKs. The dashboard reads the same `/deliveries` ledger.

---

## 3. Tenant pub-sub

A tenant-scoped subscribe/emit fabric: a tenant subscribes to event types,
and any `emit` fans out to matching subscribers.

**REST (the canonical surface):**

```
POST   /api/v1/webhooks/subscriptions      # subscribe to an event type
GET    /api/v1/webhooks/subscriptions      # list subscriptions
DELETE /api/v1/webhooks/subscriptions/{id} # unsubscribe
POST   /api/v1/webhooks/emit               # emit an event to subscribers
```

**SDK convenience methods** — `suite.webhooks.subscribe`,
`suite.webhooks.subscriptions`, `suite.webhooks.unsubscribe`, and
`suite.webhooks.emit` (Python + TypeScript) wrap those four routes.

```python
sub = await suite.webhooks.subscribe(event_type="order.created", url="https://…")
await suite.webhooks.subscriptions()
await suite.webhooks.unsubscribe(sub["id"])
await suite.webhooks.emit(event_type="order.created", body={"id": "ord_123"})
```

---

## Which one do I want?

- Receiving a webhook from Stripe/Shopify/GitHub → **inbound receiver (1)**.
- Notifying an external system when something happens in your app →
  **outbound outbox (2)**.
- Loosely coupling tenants/features inside BackAI via events →
  **tenant pub-sub (3)**.
