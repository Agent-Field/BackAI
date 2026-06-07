---
title: Notifications adapters
description: log (local) and resend (production).
sidebar:
  order: 3
---

Two adapters under `services/runtime/internal/notifications/adapters/`. Both satisfy the `notifications.Adapter` interface; the [Notifications module](../../modules/notifications/) drains the outbox uniformly.

## Selection

```yaml
modules:
  adapters:
    notifications: log     # log | resend
```

Env override:

```bash
AF_STACK_MODULE_NOTIFICATIONS=true
RESEND_API_KEY=re_...
```

When the configured adapter is `resend` and the API key is empty, the runtime fails fast with `ErrAPIKeyMissing` ("notifications/resend: api key required (set RESEND_API_KEY)").

## Capabilities matrix

| Adapter | Channels | Authentication | Use case |
|---|---|---|---|
| `log`    | any (logs only) | none | local dev, smoke tests |
| `resend` | email | `RESEND_API_KEY` Authorization bearer | production transactional email |

## When to pick which

### `log` — local dev

Writes every dispatch to the runtime log. No network calls; nothing is actually sent. The default in `docker-compose.yml`.

### `resend` — production email

Calls Resend's HTTP API. Requires `RESEND_API_KEY` (passed in the `Authorization` header). Channel is email-only; non-email rows surface as `failed` with a clear error message in the dashboard.

## Env vars

| Env | Adapter | Purpose |
|---|---|---|
| `RESEND_API_KEY` | resend | API key, sent as bearer. |

## Code map

- `adapters/log/` — local-dev adapter.
- `adapters/resend/` — Resend.com HTTP adapter (`ErrAPIKeyMissing` sentinel).

## Related

- [Notifications module](../../modules/notifications/) — service + worker.
