---
title: Swap defaults — providers, adapters, models
description: Change the LLM provider, sandbox adapter, notification adapter, or storage adapter without forking.
sidebar:
  order: 2
---

AF Stack is built around adapter interfaces. Every external dependency
(LLM provider, sandbox runtime, storage, notifications) has a default
that works out of the box, plus 2 — 4 alternatives gated by config + env.
Swapping is always: change one env or one config line, restart.

## LLM provider

Default is OpenRouter (Qwen 2.5 72B). The runtime picks the first
provider it finds:

```
OPENROUTER_API_KEY     →  routes via openrouter.ai
OPENAI_API_KEY         →  routes via api.openai.com
ANTHROPIC_API_KEY      →  routes via api.anthropic.com
```

Set whichever you have. The dashboard's **Operate → Cost** tab tags
every call with the resolved provider so you can verify.

**Per-call override:** point the OpenAI SDK at our gateway and pass any
model id from the [pricing catalog](https://github.com/Agent-Field/backai/blob/main/services/runtime/internal/pricing/pricing.go).
The catalog includes Anthropic, OpenAI, OpenRouter, and Google models;
the gateway routes based on the model prefix.

```python
client = OpenAI(base_url="http://localhost:38080/api/v1/llm", api_key="af_...")
resp = client.chat.completions.create(
    model="anthropic/claude-haiku-4-5-20251001",  # forces Anthropic
    messages=[...],
)
```

## Sandbox adapter

Default for dev is `docker` (talks to the host Docker daemon). For
production, switch to one of:

| Adapter | When to use | Env |
|---|---|---|
| `docker` | Local dev only — operator-equivalent privileges on the host | `AF_STACK_SANDBOX_ADAPTER=docker` + `docker.sock` mount |
| `gvisor` | Production with userspace kernel isolation; container-image compatible | `AF_STACK_SANDBOX_ADAPTER=gvisor` (host must have runsc installed) |
| `firecracker` | Hard multi-tenant boundaries via microVMs | `AF_STACK_SANDBOX_ADAPTER=firecracker` + Flintlock or your own broker |
| `e2b` | Managed sandbox-as-a-service | `AF_STACK_SANDBOX_ADAPTER=e2b` + `E2B_API_KEY=...` |
| `noop` | Disable entirely; sandbox endpoints return 503 | `AF_STACK_SANDBOX_ADAPTER=noop` |

The dashboard's **Build → Sandboxes** tab shows the active adapter and
its capabilities (GPU support, network egress, max timeout).

Reference: [`docs/sandbox-adapters.md`](https://github.com/Agent-Field/backai/blob/main/docs/sandbox-adapters.md).

## Notifications adapter

Default is `log` (writes to the structured logger). Switch to Resend
for real email:

```
AF_STACK_NOTIFICATIONS_ADAPTER=resend
RESEND_API_KEY=re_...
AF_STACK_NOTIFICATIONS_FROM="AF Stack <notifications@yourdomain.com>"
```

To wire SMS or push, implement the `notifications.Adapter` interface in
a new package under
`services/runtime/internal/notifications/adapters/<name>/` and add the
factory case in `cmd/af-stack/main.go`. The Resend adapter is a
30-line reference implementation.

## Storage adapter

Default for dev is `minio` (in-cluster MinIO). For production, switch to
external S3 or any S3-compatible service:

```
AF_STACK_S3_ADAPTER=s3
AF_STACK_S3_ENDPOINT=s3.amazonaws.com
AF_STACK_S3_REGION=us-east-1
AF_STACK_S3_BUCKET=your-prod-bucket
AF_STACK_S3_ACCESS_KEY=...
AF_STACK_S3_SECRET_KEY=...
```

For Cloudflare R2, set `AF_STACK_S3_ENDPOINT=<account-id>.r2.cloudflarestorage.com`
and `AF_STACK_S3_REGION=auto`. The bucket has to exist before you boot.

## Module enable / disable

Every module ships behind a flag. Defaults are conservative — most are
off until you opt in. From `config.yaml`:

```yaml
modules:
  enabled:
    multi-tenancy: true
    llm-gateway: true
    llm-cache: true
    memory: true
    sandbox: true
    notifications: true
    webhooks: true
    billing: true
    crons: true
    mcp: false      # disable a module entirely
```

Or via env (overrides config.yaml so docker-compose ergonomics work):

```
AF_STACK_MODULE_MULTI_TENANCY=true
AF_STACK_MODULE_BILLING=false
```

A disabled module's REST endpoints return 503 with a clear "module not
configured" message so the dashboard can render an empty-state CTA.

## Database

Default is `postgres://afstack:afstack@postgres:5432/afstack`. For
external Postgres:

```
AF_STACK_DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=require&pool_max_conns=25
```

The runtime needs **pgvector**. If you're using managed Postgres,
verify the extension is available (RDS: yes, since PG15; Cloud SQL: yes;
Supabase: yes; Neon: yes).

## Better-auth providers

Default is email + password. Add OAuth providers via env:

```
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
GITHUB_CLIENT_ID=...
GITHUB_CLIENT_SECRET=...
```

Better-auth detects them at boot and adds buttons to the sign-in page.

## Verify

After any swap:

```bash
docker compose down
docker compose up -d
docker compose logs runtime --tail 50 | grep -i 'adapter\|module\|provider'
```

You should see explicit lines confirming the adapter / provider chosen
("notifications: adapter=resend", "sandbox adapter ready adapter=gvisor",
etc.).
