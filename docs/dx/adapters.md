# Adapters — swapping backends + configuring credentials

Adapters let you swap the backend behind a platform primitive (storage,
secrets, the LLM gateway, notifications, …) without forking runtime code.
Two independent decisions:

1. **Which adapter is active** — always an `AF_STACK_<SLOT>_ADAPTER` env
   var. Swaps are explicit; there is no runtime auto-detection.
2. **Its credentials** — env vars, or the dashboard → **Platform →
   Integrations** page for the slots that support it.

CLI-first: `af-stack adapter list` prints the live registry (what's plugged
in per slot, its health, and the env var to change it). It reads
`GET /api/v1/admin/adapters`, so it can never drift from a static table.

## The swappable slots

| Slot | Selector env | Values | Default |
| --- | --- | --- | --- |
| Storage | `AF_STACK_S3_ADAPTER` | `minio` · `s3` · `remote` | `minio` |
| Sandbox | `AF_STACK_SANDBOX_ADAPTER` | `docker` · `gvisor` · `firecracker` · `e2b` · `remote` | `docker` |
| LLM gateway | `AF_STACK_LLM_GATEWAY_ADAPTER` | `demo` · `litellm` · `remote` | auto (demo↔litellm) |
| Notifications | `AF_STACK_NOTIFICATIONS_ADAPTER` | `log` · `resend` · `slack` · `sms`(`twilio`) · `push`(`fcm`) · `remote` | `log` |
| Billing | `AF_STACK_BILLING_ADAPTER` | `stripe` · `lago` · `none` | `stripe` |
| Secrets store | `AF_STACK_SECRETS_ADAPTER` | `vault` · `remote` (remote = roadmap) | `vault` |
| Auth | `AF_STACK_AUTH_ADAPTER` | `better-auth` (only impl) | `better-auth` |

Every selector is **validated at boot** — an unsupported value fails fast
instead of being silently ignored and falling back to a default.

Not swappable (fixed by design): the **job queue** (River), the
**reasoning layer** (AgentField), and **multimodal** (a single composition
of LiteLLM + first-party `elevenlabs`/`cartesia`/`flux`/`fal` adapters — no
`AF_STACK_MULTIMODAL_ADAPTER`).

## Credentials: env or the Integrations UI

Selecting an adapter is env-only. Its **credentials** can come from env, or
from the dashboard → **Platform → Integrations** page, which calls
`GET`/`PUT /api/v1/admin/integrations/{slot}`. UI-entered credentials are
stored in the secrets vault under `integration/{slot}/{field}` and resolved
by the runtime factories at boot.

Honest rules:

- **Not hot-reload.** UI credential changes take effect on the **next
  runtime restart**.
- **Env wins.** When a credential is set in both env and the vault, env is
  used.
- **Never returns raw secrets.** The API reports masked status only
  (set / not-set plus a short fingerprint).
- **UI-backed today:** notification channels (`resend`, `slack`, `twilio`,
  `fcm`) and the `storage` + `llm` remote URL/token. Remote **secrets** and
  **notifications** URLs are **env-only** (the secrets vault can't configure
  its own backend; notifications remote reads env at boot).

## Remote adapters

Any slot marked `remote` fronts an out-of-process sidecar speaking the
universal remote-adapter HTTP contract. Wire it with:

```bash
AF_STACK_<SLOT>_ADAPTER=remote
# URL + token per slot (see the env reference below)
```

The contract, per-slot protocols, capability probes, and the conformance
harness live under `docs/adapters/` (start at `docs/adapters/PROTOCOL.md`).

## Per-slot env reference

```bash
# Storage remote (also settable in the Integrations UI)
AF_STACK_STORAGE_REMOTE_URL=
AF_STACK_STORAGE_REMOTE_TOKEN=

# LLM gateway remote (also settable in the Integrations UI)
AF_STACK_LLM_REMOTE_URL=
AF_STACK_LLM_REMOTE_TOKEN=

# Secrets store remote (ENV-ONLY; remote is roadmap)
AF_STACK_SECRETS_REMOTE_URL=
AF_STACK_SECRETS_REMOTE_TOKEN=

# Notifications: channels
RESEND_API_KEY=
AF_STACK_SLACK_WEBHOOK_URL=
AF_STACK_TWILIO_ACCOUNT_SID=
AF_STACK_TWILIO_AUTH_TOKEN=
AF_STACK_TWILIO_FROM_NUMBER=
AF_STACK_FCM_PROJECT_ID=
AF_STACK_FCM_ACCESS_TOKEN=      # short-lived OAuth token minted at your ops layer
# Notifications remote (ENV-ONLY; empty URL falls back to the log adapter)
AF_STACK_NOTIFICATIONS_ADAPTER_URL=
AF_STACK_NOTIFICATIONS_ADAPTER_TOKEN=
```

See `.env.example` for the full, commented list and
[../adapters/README.md](../adapters/README.md) for the per-slot protocol
specs and the conformance harness.

## Known limitations

- **Remote secrets is roadmap.** The `remote` secrets adapter is
  selectable, validated, and capability-probed, but the server currently
  binds the concrete vault type — a remote backend cannot yet fully back
  `/api/v1/secrets` end-to-end. Treat `vault` as the only production store
  today. Generalizing the server's secrets dependency to the `Store`
  interface is the follow-up.
- **FCM push** takes an OAuth **access token** (`AF_STACK_FCM_ACCESS_TOKEN`),
  not a service-account JSON; mint it at the boot/ops layer.
