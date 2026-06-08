# Notification Adapters

Notification adapters deliver outbound email, SMS, and push messages
from the notifications outbox.

## Active selector

Set:

```bash
AF_STACK_NOTIFICATIONS_ADAPTER=log
```

Supported today:

| Adapter | Use |
|---|---|
| `log` | Development default; writes notifications to logs |
| `resend` | Email delivery through Resend; requires `RESEND_API_KEY` |

Planned:

| Adapter | Channel |
|---|---|
| `postmark` | Email |
| `sendgrid` | Email |
| `ses` | Email |
| `mailgun` | Email |
| `twilio` | SMS |
| `fcm` | Push |
| `onesignal` | Push |

## Provider env

```bash
RESEND_API_KEY=
AF_STACK_NOTIFICATIONS_FROM=
```
