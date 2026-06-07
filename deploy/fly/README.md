# Fly.io deploy

What this gets you:

- Runtime (Go) + dashboard (Next.js) on Fly Machines v2.
- Auto-TLS, auto-scaling, health-gated rollouts.
- External Postgres (Neon/Supabase) + external S3 (Tigris/R2/S3).

## Why two apps

Fly's per-app routing maps `:80/:443` to ONE internal port. Runtime and
dashboard live on different ports, so we ship them as two Fly apps and
wire the dashboard to the runtime over Fly's private `6PN` network
(`<app>.flycast`). Cheaper, simpler, and the native idiom in 2026.

If you really want one machine, see `Dockerfile.fly` — it bundles both
processes behind `s6-overlay`. We don't recommend it: less observable,
harder to scale independently.

## Prerequisites

- `flyctl` >= 0.4
- External Postgres URL (`postgres://user:pass@host:5432/db?sslmode=require`)
- External S3 creds (Tigris, R2, AWS S3, or any S3-compatible)
- OpenRouter / Anthropic / OpenAI key
- E2B API key (sandbox adapter — see `docs/sandbox-adapters.md`)

## Walkthrough

```bash
# 1. Clone + cd
git clone https://github.com/<you>/af-stack && cd af-stack

# 2. Template fly.toml + fly.dashboard.toml
#    Replace <app-name>, <dashboard-app-name>, <primary-region>, <runtime-app-name>.
$EDITOR deploy/fly/fly.toml deploy/fly/fly.dashboard.toml

# 3. Launch runtime app (no deploy yet — we set secrets first).
flyctl launch \
  --config deploy/fly/fly.toml \
  --no-deploy \
  --copy-config

# 4. Set runtime secrets.
flyctl secrets set --app <app-name> \
  AF_STACK_DATABASE_URL="postgres://..." \
  AF_STACK_KMS_KEY="$(openssl rand -hex 32)" \
  AF_STACK_AUTH_SECRET="$(openssl rand -hex 32)" \
  AF_STACK_S3_ENDPOINT="https://fly.storage.tigris.dev" \
  AF_STACK_S3_BUCKET="af-stack-prod" \
  AF_STACK_S3_ACCESS_KEY="..." \
  AF_STACK_S3_SECRET_KEY="..." \
  OPENROUTER_API_KEY="..." \
  E2B_API_KEY="..."

# 5. Deploy runtime.
flyctl deploy --config deploy/fly/fly.toml --app <app-name>

# 6. Launch dashboard app.
flyctl launch \
  --config deploy/fly/fly.dashboard.toml \
  --no-deploy \
  --copy-config

# 7. Set dashboard secrets (same DB as runtime; better-auth shares the pool).
flyctl secrets set --app <dashboard-app-name> \
  DATABASE_URL="postgres://..." \
  AF_STACK_AUTH_SECRET="$(flyctl secrets list --app <app-name> | grep AF_STACK_AUTH_SECRET)" \
  BETTER_AUTH_URL="https://<dashboard-app-name>.fly.dev"

# 8. Deploy dashboard.
flyctl deploy --config deploy/fly/fly.dashboard.toml --app <dashboard-app-name>
```

## Validation

```bash
flyctl status --app <app-name>
curl https://<app-name>.fly.dev/health   # {"status":"ok",...}
curl https://<app-name>.fly.dev/ready    # 200 when DB+AF are reachable
curl https://<dashboard-app-name>.fly.dev/  # dashboard login page
```

## Common pitfalls

- `release_command` runs `af-stack migrate up` before each deploy. If your
  Postgres is unreachable from the Fly region, this hangs the deploy. Fix
  by deploying with `--strategy=immediate` once, then re-enabling.
- The dashboard CANNOT reach the runtime over the public Fly URL from
  inside `RUNTIME_URL` — use `<runtime-app-name>.flycast` (the 6PN private
  hostname). Save egress bandwidth and lower latency.
- Auto-scaling `min_machines_running = 0` will cold-start; we hardcode `1`
  to keep `/ready` returning 200. Set to 0 only if you're fine with 1-2s
  cold starts on the first request after idle.
- Sandbox volume: only uncomment the `[mounts]` block if you switch off
  `e2b` and run on-box sandboxes (gvisor). Even then, sandbox artefacts
  should usually go to S3, not local disk.
