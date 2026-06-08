# Adapters — swapping backends for existing primitives

An adapter is a swappable implementation behind a Go interface for an
**existing** platform primitive. NOT a plugin. Operator picks via env
var. We ship multiple in-tree; the user can add more.

## What's currently adapter-swappable

| Primitive | Adapters | Env var | Default |
|---|---|---|---|
| **Storage** | `minio`, `s3` (covers AWS S3 / R2 / GCS / Azure Blob via S3 API) | `AF_STACK_S3_ADAPTER` | `minio` (dev) |
| **Sandbox** | `docker`, `gvisor`, `firecracker`, `e2b` | `AF_STACK_SANDBOX_ADAPTER` | `docker` (dev) |
| **Notifications email** | `log`, `resend` | `AF_STACK_NOTIFICATIONS_ADAPTER` | `log` |
| **Billing** | `stripe`, `lago`, `none` | `AF_STACK_BILLING_ADAPTER` | `stripe` |
| **LLM provider routing** | LiteLLM internal — picks model based on call | (in `litellm-config.yaml`) | (always on) |

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

The dashboard's `Build → Modules` tab shows the live adapter choices.
will eventually add a dedicated `Infrastructure → Adapters` page (per
`POSITIONING.md` Part 3 B2) that lists every primitive + every
available adapter + the active one + a link to docs.

That page is **read-only** — config still happens in env. The dashboard
is the observation surface; the config is in your fork's `.env`.
