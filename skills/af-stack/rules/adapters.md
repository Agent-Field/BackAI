# Adapters — swapping backends for existing primitives

An adapter is a swappable implementation behind a Go interface for an
**existing** platform primitive. NOT a plugin. Operator picks via env
var. We ship multiple in-tree; the user can add more.

## What's currently adapter-swappable

| Primitive | Adapters | Env var | Default |
|---|---|---|---|
| **Storage** | `minio`, `s3` (covers AWS S3 / R2 / GCS / Azure Blob via S3 API), `remote` | `AF_STACK_S3_ADAPTER` | `minio` (dev) |
| **Sandbox** | `docker`, `gvisor`, `firecracker`, `e2b`, `remote` | `AF_STACK_SANDBOX_ADAPTER` | `docker` (dev) |
| **Notifications** | `log`, `resend` (email), `slack`, `sms`/`twilio`, `push`/`fcm`, `remote` | `AF_STACK_NOTIFICATIONS_ADAPTER` | `log` |
| **Billing** | `stripe`, `lago`, `none` | `AF_STACK_BILLING_ADAPTER` | `stripe` |
| **LLM gateway** | `demo`, `litellm`, `remote` | `AF_STACK_LLM_GATEWAY_ADAPTER` | auto (demo↔litellm via `AF_STACK_DEMO_MODE`) |
| **Secrets store** | `vault`, `remote` (remote is roadmap — see below) | `AF_STACK_SECRETS_ADAPTER` | `vault` |
| **Auth** | `better-auth` (only impl today; now validated, not silently ignored) | `AF_STACK_AUTH_ADAPTER` | `better-auth` |

Notes on the newer slots:

- **LLM gateway** is a real selector now (it used to be "always LiteLLM").
  `litellm` still owns per-call model routing via `litellm-config.yaml`;
  the env picks demo vs litellm vs a remote gateway. Provider *keys* stay
  in `.env`.
- **Secrets store `remote`** is selectable + validated + capability-probed,
  but the server still binds the concrete vault type, so a remote backend
  cannot yet fully back `/api/v1/secrets` end-to-end. **Roadmap**, not a
  finished capability — treat `vault` as the only production store today.
- **Auth** has exactly one implementation. The env exists so an unsupported
  value fails fast; it does **not** imply a second auth backend ships.
- **Multimodal is NOT env-swappable** — it's a single composition
  (LiteLLM for the OpenAI catalog + first-party `elevenlabs`/`cartesia`/
  `flux`/`fal` adapters keyed by provider env). There is no
  `AF_STACK_MULTIMODAL_ADAPTER`.
- **Jobs/queue is NOT swappable** — River-backed, single impl by design.
- Every selector value is validated at boot; an unsupported
  `AF_STACK_<SLOT>_ADAPTER` fails fast instead of silently defaulting.
- Each swappable slot also accepts `remote` (an out-of-process adapter over
  the remote protocol; see `docs/adapters/PROTOCOL.md`).

Adapter **credentials** can come from env or the dashboard → Platform →
Integrations page (`PUT /api/v1/admin/integrations/{slot}`, stored in the
secrets vault). Env wins; UI changes apply on the next runtime restart.

## The adapter pattern

Each adapter implements an interface. Example for Sandbox:

```go
package sandbox

type Sandbox interface {
    Run(ctx context.Context, spec RunSpec) (*RunResult, error)
    Stream(ctx context.Context, spec RunSpec) (<-chan LogLine, *RunResult, error)
    Stop(ctx context.Context, jobID string) error
    Capabilities() Capabilities
}
```

Adapters live under `services/runtime/internal/<area>/adapters/<id>/`:

```
internal/sandbox/
├── interface.go             # the Sandbox interface
├── factory.go               # picks the adapter from env
└── adapters/
    ├── docker/              # for dev
    ├── gvisor/              # production single-host
    ├── firecracker/         # hard multi-tenancy
    └── e2b/                 # managed remote
```

The factory looks at `AF_STACK_SANDBOX_ADAPTER` and returns the
matching impl. Routes call the interface, not the adapter directly.

## When to swap an adapter

| Situation | Action |
|---|---|
| Production deploy on AWS | `AF_STACK_S3_ADAPTER=s3` + `AWS_*` env |
| Production deploy with Cloudflare R2 | `AF_STACK_S3_ADAPTER=s3` + `AWS_ENDPOINT_URL=https://...r2.cloudflarestorage.com` |
| Hard multi-tenant prod | `AF_STACK_SANDBOX_ADAPTER=gvisor` (or `firecracker`) |
| Managed sandbox (skip ops) | `AF_STACK_SANDBOX_ADAPTER=e2b` + `E2B_API_KEY` |
| Real email | `AF_STACK_NOTIFICATIONS_ADAPTER=resend` + `RESEND_API_KEY` |
| OSS billing (no Stripe) | `AF_STACK_BILLING_ADAPTER=lago` (not yet) |
| No billing at all | `AF_STACK_BILLING_ADAPTER=none` |

Operator restarts after the env change. Dashboard's `Build → Modules`
shows the active adapter.

## When to ADD a new adapter

When the existing set doesn't cover a need. E.g.:

- Adding `azure-blob` storage with native auth instead of S3 API.
- Adding `modal` sandbox.
- Adding `postmark` or `sendgrid` for email.
- Adding `paddle` for billing.

Steps to add:

1. Implement the interface under `internal/<area>/adapters/<your-id>/`.
2. Register it in the factory's switch statement.
3. Document the required env vars in `.env.example` and `docs/`.
4. Add to the OSS-AUDIT table.
5. Add tests using the existing adapter test fixtures.

**Don't** add adapter-switching logic that depends on runtime
detection (e.g. "use S3 if `AWS_REGION` is set"). Adapter choice is
explicit, via the env var.

## Anti-patterns

| Anti-pattern | Why wrong | Correct |
|---|---|---|
| `if AWS_REGION { use s3 } else { use minio }` | Implicit choice; deploys non-deterministic | Operator sets `AF_STACK_S3_ADAPTER` |
| Adapter that falls back to another adapter on failure | Hides operational issues | Surface the error; let the operator decide |
| Adding business logic in the adapter | Adapters are thin shims over the underlying primitive | Business logic stays in the layer above |
| Adapter that wraps multiple primitives | Mixes concerns | One adapter per interface |
| Magic env var detection without env var | Surprising deploys | Always an explicit `AF_STACK_*_ADAPTER` |
| Adapter that depends on another adapter's choice | Coupling | Adapters independent |

## What about hooks?

Adapters swap the **backend implementation** of a primitive. Hooks
intercept **the flow through it**. They're different.

If the user wants to "add PII redaction before LLM calls," that's a hook
(`llm.pre_call`), not an adapter. Hooks engine lives at
`services/runtime/internal/hooks/`.

The canonical hook points:
- `gateway.pre_auth`, `gateway.post_auth`
- `af.pre_execute`, `af.post_execute`
- `llm.pre_call`, `llm.post_call`
- `sandbox.pre_run`, `sandbox.post_run`
- `storage.pre_upload`
- `notifications.pre_send`
- `billing.pre_charge`
- `tenant.created`

If your case fits a hook, use that. If it needs a different backend
(swap S3 → R2), use an adapter swap.

## Dashboard surfacing

Two surfaces:

- **Which adapter is active** is observation-only in the dashboard, backed
  by the live registry at `GET /api/v1/admin/adapters` (also what
  `af-stack adapter list` reads). Selecting an adapter still happens via the
  `AF_STACK_<SLOT>_ADAPTER` env var — swaps are explicit, in your fork's
  `.env`.
- **Adapter credentials** *can* be set from the dashboard → `Platform →
  Integrations` page (`GET`/`PUT /api/v1/admin/integrations/{slot}`), which
  stores them in the secrets vault under `integration/{slot}/{field}`. The
  API returns masked status only (never raw values), env wins over UI
  values, and UI changes apply on the **next runtime restart** (not
  hot-reload). This covers notification channels + the `storage`/`llm`
  remote URL/token; remote `secrets` and `notifications` URLs stay
  env-only.
