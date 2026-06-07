---
title: Module — Notifications
description: Outbox-style notification layer. Insert + background worker drains via log or Resend.
sidebar:
  order: 6
---

Outbox-style notification dispatcher. `Service.Send` inserts a row at `status=queued`; a background worker drains every ~2s through the configured adapter (log, Resend, ...). Status transitions are atomic so the dashboard sees one of `queued / sending / sent / failed / skipped`.

## What it does

`notifications.Service` exposes `Send` + read APIs. The worker (a single goroutine) polls `suite_notifications` and dispatches each row through the `notifications.Adapter` interface. Wire shapes (`NotificationSchema`, `NotificationListSchema`, `SendNotificationInputSchema`, `NotificationStatsSchema`) live in `apps/dashboard/src/lib/api.ts`.

When no adapter is wired, `POST /api/v1/notifications` returns `503`; reads degrade to empty pages.

## Configuration

```yaml
modules:
  adapters:
    notifications: log     # log | resend
```

Env override (any module config):

```bash
AF_STACK_MODULE_NOTIFICATIONS=true
RESEND_API_KEY=re_...      # required for adapter=resend
```

See [Notifications adapters](../../adapters/notifications/).

## REST endpoints

Registered in `services/runtime/internal/server/notifications.go`:

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/notifications` | Enqueue a notification. |
| `GET` | `/api/v1/notifications` | List notifications (paginated). |
| `GET` | `/api/v1/notifications/stats` | Counts by status. |
| `GET` | `/api/v1/notifications/{id}` | Get a single notification. |

## Database tables

Owned by migration `00009_notifications.sql`:

- `suite_notifications` — id, tenant, channel, recipient, subject, body, status, attempts, last_error, created_at, sent_at.

## Env vars

| Env | Purpose |
|---|---|
| `AF_STACK_MODULE_NOTIFICATIONS` | Enable / disable. |
| `RESEND_API_KEY` | Required for the Resend adapter. |

## Code map

- `interface.go` — `Service`, `Adapter`, wire shapes.
- `service.go` — `Send` (insert row), list, stats.
- `recorder.go` — DB writes.
- `worker.go` — drain loop.
- `adapters/log/` — local-dev adapter (logs only).
- `adapters/resend/` — Resend.com HTTP adapter.

## Related

- Fires [`notifications.pre_send`](../../hooks/#notificationspresend) (scaffolded; not yet wired in v1).
- Adapter detail: [Notifications adapters](../../adapters/notifications/).
