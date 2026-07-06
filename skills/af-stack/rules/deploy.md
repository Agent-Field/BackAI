# Deploy — production deployment of the fork

AF Stack ships 5 deploy paths. Each is the same code; the operator picks
based on familiarity. The repo IS the product — no hosted version.

## Deploy targets

| Target | When | Cost ballpark | Where |
|---|---|---|---|
| `docker compose up` | Dev / local testing | Free | Anywhere |
| `docker-compose.prod.yml` + Caddy | Smallest production deploys (single VPS) | $5–20/mo VPS | `docker-compose.prod.yml` |
| **Helm** | Kubernetes (GKE / EKS / AKS / self-managed) | Cluster + DB + storage | `deploy/helm/` |
| **Fly.io** | App platform; multi-region; great DX | $1.94/mo per machine + DB | `deploy/fly/` |
| **Railway** | Click-deploy template | Pay-as-you-go | `deploy/railway/` |
| **Render** | Blueprint-based | $7+/mo per service | `deploy/render/` |

## What ships in the deploy

A single deploy includes:

- AF Stack runtime (Go binary)
- AgentField control plane (container)
- LiteLLM sidecar
- Postgres + pgvector (or external if `AF_STACK_DATABASE_URL` points outside)
- MinIO (or external if `AF_STACK_S3_ADAPTER=s3`)
- Dashboard (Next.js)
- Customer app (Next.js)
- Your agents (one container per agent)
- Your workload modules (one container per Python sidecar)
- Caddy (in prod compose) for TLS + reverse proxy

## Required env (the minimum)

```bash
# .env

# LLM provider (at least one)
OPENROUTER_API_KEY=sk-or-...

# Auth secret (production: 32-byte random hex)
AF_STACK_AUTH_SECRET=...

# KMS key for secrets vault (production: 32-byte random hex)
AF_STACK_KMS_KEY=...

# Multi-tenancy (toggle)
AF_STACK_MODULE_MULTI_TENANCY=true

# Better-auth public URL
BETTER_AUTH_URL=https://admin.yourbrand.com

# Optional: OAuth providers (set the ones you want)
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
GITHUB_CLIENT_ID=...
GITHUB_CLIENT_SECRET=...

# Optional: external providers
RESEND_API_KEY=re_...        # for email
STRIPE_SECRET_KEY=sk_...      # for billing
E2B_API_KEY=...              # for managed sandbox
```

## Per-target gotchas

### Helm

```bash
helm install your-name ./deploy/helm/af-stack -f values-prod.yaml
```

- `values-prod.yaml` assumes external Postgres + S3 + Redis.
- `values-dev.yaml` runs everything in-cluster.
- HPA, NetworkPolicy, PDB, ServiceMonitor are pre-configured.
- Production needs a real `AF_STACK_KMS_KEY` (lose it → secrets vault unreadable).
- Run `helm lint deploy/helm/af-stack` before deploying.

### Fly.io

```bash
flyctl launch --from <repo>
flyctl secrets set OPENROUTER_API_KEY=... AF_STACK_AUTH_SECRET=...
flyctl deploy
```

- Two apps: dashboard / customer-app share the same Postgres via flycast.
- Multi-region: replicate the DB; runtime is stateless.
- Set secrets via `flyctl secrets`, not committed files.

### Railway

- Click the template, set env vars in the web UI.
- Railway provisions Postgres for you.
- Service-to-service URLs use Railway DNS.

### Render

- Blueprint deploys runtime + Postgres + Caddy.
- Set env vars in the Render dashboard.
- Custom domains via Render's TLS layer.

### docker-compose.prod.yml + Caddy

```bash
docker compose -f docker-compose.prod.yml up -d
```

- Caddy auto-renews TLS for the domains you list.
- Single VPS: 2 vCPU + 4 GB RAM minimum.
- Backups: see `scripts/backup.sh` (PG dump + MinIO sync).

## Graceful shutdown

The runtime handles SIGTERM:

1. `/health` becomes 503 (drain)
2. HTTP server drains in-flight requests (~30s)
3. Workers (River, crons) cancel cleanly
4. DB pool closes

Kubernetes / Fly / Railway / Render all send SIGTERM on rollouts. Don't
need to do anything special.

## Multi-replica considerations

| Concern | How it's handled |
|---|---|
| Job queue | River + PG `FOR UPDATE SKIP LOCKED` — multi-replica safe |
| Cron | Same — multi-replica safe |
| Webhook delivery | Native in-process outbox — PG-backed queue + tick worker, multi-replica safe |
| Tenant resolver | Stateless |
| Cost ledger | PG-backed, no in-memory state |
| LiteLLM rate limit | eventually upstream LiteLLM virtual keys (not yet) |

You can scale runtime + dashboard + customer-app horizontally without
state issues.

## Multi-region

Roadmap, not v1. The path:
- Pin Postgres to one region; use read replicas in others.
- Runtime stateless; deploy in each region.
- DNS routing via Fly / Cloudflare.
- Object storage replication via S3 / R2 native.

Don't ship multi-region until customer demand justifies it.

## After first deploy — bootstrap

On a fresh deploy:

1. First sign-up auto-promotes to the operator role (planned; today
   it's the "default tenant" magic).
2. Dashboard at `https://admin.yourbrand.com`.
3. Customer app at `https://app.yourbrand.com`.
4. API at `https://api.yourbrand.com`.

The "Getting Started" panel (planned) walks the operator through:
create tenant → issue API key → set budget → open customer app.

## Backups + DR

- **Postgres**: `scripts/backup.sh` does `pg_dump` to a backup bucket.
- **MinIO / S3**: native replication or `mc mirror`.
- **AgentField**: backs up via its own Postgres (`agentfield` DB on the
  same instance).
- **Secrets vault**: encrypted with `AF_STACK_KMS_KEY` — back up the
  KMS key separately (lose it = unreadable secrets).

`scripts/restore.sh` is the reverse path. Test it before you need it.

## What NOT to deploy

- **Dev `docker.sock` mounting in production** — use gVisor or
  Firecracker for sandboxes.
- **`AF_STACK_KMS_KEY=dev-secret-change-me-in-prod`** — replace with a
  real 32-byte hex value.
- **Default better-auth signing key** — `AF_STACK_AUTH_SECRET` must be a
  real random value.

The `.env.example` flags these with `change-me-in-prod` markers.
