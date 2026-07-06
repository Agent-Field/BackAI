# Notification Adapters

Notification adapters deliver outbound email, SMS, and push messages
from the notifications outbox.

## Active selector

Set:

```bash
AF_STACK_NOTIFICATIONS_ADAPTER=log
```

Supported today:

| Adapter | Channel | Requires |
|---|---|---|
| `log` | — | Development default; writes notifications to logs |
| `resend` | Email | `RESEND_API_KEY` |
| `slack` | Chat | `AF_STACK_SLACK_WEBHOOK_URL` |
| `sms` (alias `twilio`) | SMS | Twilio account SID / auth token / from-number |
| `push` (alias `fcm`) | Push | FCM project id + access token |
| `remote` | any | An out-of-process adapter speaking the [remote protocol](PROTOCOL.md) |

Planned:

| Adapter | Channel |
|---|---|
| `postmark` | Email |
| `sendgrid` | Email |
| `ses` | Email |
| `mailgun` | Email |
| `onesignal` | Push |

## Channel env

Set the selector to the channel you want, then supply its credentials. All
channel credentials below can also be entered from the dashboard →
**Platform → Integrations** (stored in the secrets vault); env wins when
both are present, and UI-set values take effect on the **next runtime
restart**.

```bash
# Email (Resend)
RESEND_API_KEY=
AF_STACK_NOTIFICATIONS_FROM=

# Slack (incoming webhook)
AF_STACK_SLACK_WEBHOOK_URL=

# SMS (Twilio)
AF_STACK_TWILIO_ACCOUNT_SID=
AF_STACK_TWILIO_AUTH_TOKEN=
AF_STACK_TWILIO_FROM_NUMBER=
AF_STACK_TWILIO_BASE_URL=      # optional; override the Twilio API base URL

# Push (Firebase Cloud Messaging)
AF_STACK_FCM_PROJECT_ID=
AF_STACK_FCM_ACCESS_TOKEN=
AF_STACK_FCM_BASE_URL=         # optional; override the FCM API base URL
```

> **FCM tokens:** `AF_STACK_FCM_ACCESS_TOKEN` is a short-lived OAuth access
> token, not a service-account JSON. Minting that token from the service
> account is expected to happen at your boot/ops layer.

## Remote adapter

```bash
AF_STACK_NOTIFICATIONS_ADAPTER=remote
AF_STACK_NOTIFICATIONS_ADAPTER_URL=https://notify-adapter.example.com
AF_STACK_NOTIFICATIONS_ADAPTER_TOKEN=<bearer-token>
```

Remote notifications credentials are **env-only**. If the URL is empty the
runtime falls back to the `log` adapter rather than failing.
