# Notifications Adapter — Protocol v1

> Inherits from [`PROTOCOL.md`](../PROTOCOL.md).
>
> **Slot:** `notifications` · **Base path:** `/v1` · **Go interface:**
> `services/runtime/internal/notifications/Adapter`

## Purpose

Notifications adapters send transactional messages (email, SMS, push,
chat-app). Built-in adapters: `log` (no-op), `resend` (email via Resend).

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/messages` | Send a notification, return adapter message id |
| `GET` | `/v1/messages/{id}` | Check delivery status (optional, see §3) |
| `POST` | `/v1/messages/{id}/retry` | Re-attempt delivery (optional) |
| `GET` | `/v1/capabilities` | Capability declaration |
| `GET` | `/healthz` | Liveness |
| `GET` | `/v1/info` | Optional metadata |

## 1. `POST /v1/messages`

Send one notification.

**Request body**:

```json
{
  "channel": "email",
  "to": ["alice@example.com"],
  "cc": [],
  "bcc": [],
  "from": "ops@acme.com",
  "subject": "Welcome to BackAI",
  "body_text": "Hi Alice, ...",
  "body_html": "<p>Hi Alice, ...</p>",
  "reply_to": "noreply@acme.com",
  "template_id": "",
  "template_vars": {},
  "metadata": {
    "tenant_id": "acme",
    "category": "welcome"
  }
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `channel` | enum | yes | `email`, `sms`, `push`, `slack`, `log`. Adapter rejects channels it doesn't support with `unsupported_channel`. |
| `to` | string[] | yes | Primary recipients. Format depends on channel. |
| `cc` | string[] | optional | Carbon-copy recipients (email channel only). Adapter rejects with `unsupported_parameter` if the channel doesn't support cc. |
| `bcc` | string[] | optional | Blind carbon-copy recipients (email channel only). Adapter rejects with `unsupported_parameter` if not supported. |
| `from` | string | when channel requires it | Email sender, SMS sender id, etc. |
| `subject` | string | for email | |
| `body_text` | string | one of body_* | Plain-text body. |
| `body_html` | string | one of body_* | HTML body (for email). |
| `reply_to` | string | optional | |
| `template_id` | string | optional | Provider-specific template id. If set, `body_text`/`body_html` may be empty. |
| `template_vars` | object | optional | Variable substitutions for `template_id`. |
| `metadata` | object | optional | Free-form. Adapter SHOULD forward to provider where supported (e.g., Resend tags). |

**Response (200 OK)**:

```json
{
  "id": "01HZAB...",
  "provider_message_id": "re_abc123",
  "status": "sent",
  "sent_at": "2026-06-15T10:00:00Z"
}
```

| Field | Notes |
|---|---|
| `id` | Adapter's internal id (may equal `provider_message_id`). |
| `provider_message_id` | Upstream provider's id (useful for deep-linking). Empty if the adapter doesn't have one (e.g., `log`). |
| `status` | `sent` (handed off to provider), `queued` (adapter buffered, not yet sent), `failed` (synchronous failure with `error_code` in RFC 7807 body). |
| `sent_at` | When the adapter handed it off; null if `queued`. |

Adapters MAY return `202 Accepted` instead of `200 OK` if delivery is
fully async; the response shape is the same.

## 2. `GET /v1/messages/{id}` (optional)

If the adapter tracks delivery status, return it:

```json
{
  "id": "01HZAB...",
  "provider_message_id": "re_abc123",
  "status": "delivered",
  "sent_at": "2026-06-15T10:00:00Z",
  "delivered_at": "2026-06-15T10:00:02Z",
  "bounced": false,
  "complained": false,
  "last_error": null
}
```

`status` values: `queued`, `sent`, `delivered`, `bounced`, `failed`.

Adapters that don't track post-send status MAY omit this endpoint; the
runtime treats `404` here as "status unknown" and surfaces the original
send response.

If the endpoint exists, `capabilities.tracks_delivery_status` MUST be
`true`.

## 3. `POST /v1/messages/{id}/retry` (optional)

Re-send a previously-failed message. Returns the same shape as the
original `POST /v1/messages`. Returns `409 Conflict` with
`code: "already_delivered"` if the message succeeded.

If `capabilities.supports_retry` is `false`, the runtime never calls
this endpoint.

## 4. `GET /v1/capabilities`

```json
{
  "name": "resend",
  "version": "1.0.0",
  "slot": "notifications",
  "protocol_version": "v1",
  "vendor": "BackAI",
  "capabilities": {
    "channels": ["email"],
    "supports_html": true,
    "supports_templates": true,
    "supports_cc_bcc": true,
    "tracks_delivery_status": true,
    "supports_retry": true,
    "max_recipients_per_message": 50,
    "max_body_size_bytes": 1048576,
    "rate_limit_per_minute": 100,
    "supports_metadata_passthrough": true
  }
}
```

| Key | Type | Meaning |
|---|---|---|
| `channels` | string[] | Channels this adapter handles. The runtime rejects messages whose channel isn't listed before calling. |
| `supports_html` | bool | Whether `body_html` is honored. |
| `supports_templates` | bool | Whether `template_id` is honored. |
| `supports_cc_bcc` | bool | Whether `cc` and `bcc` fields are honored (email channel). |
| `tracks_delivery_status` | bool | If `true`, `GET /v1/messages/{id}` works. |
| `supports_retry` | bool | If `true`, `POST /v1/messages/{id}/retry` works. |
| `max_recipients_per_message` | int | Adapter's hard limit on `to` size. |
| `max_body_size_bytes` | int | Body size cap. |
| `rate_limit_per_minute` | int | Adapter's known upstream rate limit. The runtime SHOULD self-throttle. |
| `supports_metadata_passthrough` | bool | Whether `metadata` is forwarded to the provider (e.g., as tags). |

## 5. Error codes

| Code | HTTP | Meaning |
|---|---|---|
| `unsupported_channel` | 422 | Channel not in `capabilities.channels`. |
| `invalid_recipient` | 400 | `to` address malformed. |
| `body_too_large` | 413 | Exceeds `max_body_size_bytes`. |
| `template_not_found` | 404 | `template_id` unknown to the upstream. |
| `provider_error` | 502 | Upstream provider returned an error; `detail` carries provider message. |
| `rate_limited` | 429 | Adapter's upstream throttled the call. Include `Retry-After`. |
| `quota_exceeded` | 429 | Adapter-level quota exhausted. |
| `adapter_unavailable` | 503 | Backend provider unreachable. |
| `unauthorized` | 401 | Bearer token rejected. |
| `already_delivered` | 409 | Retry called on a delivered message. |
| `internal_error` | 500 | Catch-all. |

## 6. Behavior notes

- **No PII in logs.** Adapters MUST NOT log recipient addresses or
  message bodies. Log `id`, `channel`, `status` only.
- **Bounce handling.** Adapters that track bounces MUST expose them via
  `GET /v1/messages/{id}`; the runtime surfaces them in the dashboard.
- **Rate limiting.** When the adapter detects upstream throttling,
  return `429 + Retry-After`. The runtime backs off automatically.

## 7. Mapping back to the Go interface

The current Go `notifications.Adapter` is minimal (`Send`, `Name`).
The remote shim implements both:

| Go method | HTTP call |
|---|---|
| `Send(ctx, n)` | `POST /v1/messages` → returns `provider_message_id` |
| `Name()` | cached from `GET /v1/capabilities` → `name` |

When future Go methods are added (e.g., `GetStatus`), they map to
`GET /v1/messages/{id}`.

## 8. Conformance checklist

- [ ] `POST /v1/messages` with channel=`log` and a simple body returns `200` + id
- [ ] `POST /v1/messages` with an unsupported channel returns `422 + unsupported_channel`
- [ ] `POST /v1/messages` with no body returns `400`
- [ ] Idempotent retry: same `X-BackAI-Idempotency-Key` returns identical response
- [ ] If `tracks_delivery_status=true`, `GET /v1/messages/{id}` for the just-sent message returns its status
- [ ] If `tracks_delivery_status=false`, `GET /v1/messages/{id}` returns `404` and that's OK
- [ ] `/v1/capabilities` lists every channel the adapter actually accepts
- [ ] Bearer auth enforced
