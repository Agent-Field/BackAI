# Deploy BackAI

Five officially-supported deploy targets. Pick the one matching your ops
posture and skip the rest.

| Target              | Path                               | Best for                                 |
| ------------------- | ---------------------------------- | ---------------------------------------- |
| Helm (Kubernetes)   | `deploy/helm/af-stack/`            | You have a cluster, run multi-tenant SaaS |
| Fly.io              | `deploy/fly/`                      | Solo founders, fast global rollout       |
| Railway             | `deploy/railway/`                  | One-click SupportDesk demo, bundled Postgres |
| Render              | `deploy/render/`                   | Blueprint deploys, GitHub-driven autodeploy |
| Docker Compose (prod) | `docker-compose.prod.yml` (root) | Single VPS, external Postgres + S3       |

## Decision matrix

You want…                                          | Use this
-------------------------------------------------- | --------
… production HA on Kubernetes                      | Helm
… "deploy from git in 5 minutes"                   | Render or Railway
… global edge deploy with Fly Machines             | Fly
… one Hetzner box and total control                | Compose
… to dogfood on your laptop                        | dev `docker-compose.yml`

## Required env vars (cheat sheet)

Every production target eventually needs these. The Railway SupportDesk first
run can start without provider keys, S3, or sandbox credentials:

| Var                          | What                                          |
| ---------------------------- | --------------------------------------------- |
| `AF_STACK_DATABASE_URL`      | Postgres URL with pgvector enabled            |
| `AF_STACK_KMS_PROVIDER`      | `env`, `aws`, `gcp`, or `azure`               |
| `AF_STACK_KMS_KEY`           | `openssl rand -hex 32` when provider is `env` |
| `AF_STACK_KMS_ENCRYPTED_DATA_KEY` | base64 cloud-KMS-wrapped 32-byte data key for BYOK |
| `AF_STACK_AUTH_SECRET`       | `openssl rand -hex 32` — session signing key  |
| `AF_STACK_S3_ENDPOINT`       | S3-compatible endpoint                        |
| `AF_STACK_S3_BUCKET`         | Bucket for sandbox artefacts + uploads        |
| `AF_STACK_S3_ACCESS_KEY`     | S3 access key                                 |
| `AF_STACK_S3_SECRET_KEY`     | S3 secret key                                 |
| `AF_STACK_SANDBOX_ADAPTER`   | `e2b` (recommended) / `gvisor` / `docker`     |
| `E2B_API_KEY`                | Required when adapter=e2b                     |
| `OPENROUTER_API_KEY`         | LLM provider (or `OPENAI_API_KEY` /           |
|                              | `ANTHROPIC_API_KEY`)                          |

See [Demo Mode And Real Provider Mode](demo-mode.md) for the no-key
SupportDesk path and the LiteLLM-backed provider path.

Compose target also needs:

| Var                          | What                                          |
| ---------------------------- | --------------------------------------------- |
| `AF_STACK_DOMAIN`            | Public hostname for Caddy auto-TLS            |
| `ACME_EMAIL`                 | Let's Encrypt registration email              |
| `AGENTFIELD_STORAGE_POSTGRES_URL` | Postgres URL for the AF control plane     |

See each target's `README.md` for platform-specific extras (`fromService`
references on Render, `${{ runtime.X }}` references on Railway, etc.).

## Quick start by target

```bash
# Helm
helm install af-stack ./deploy/helm/af-stack \
  --set runtime.image.tag=v0.14.0 \
  --set externalDatabase.url="$AF_STACK_DATABASE_URL" \
  --set externalS3.endpoint="$AF_STACK_S3_ENDPOINT"

# Fly
flyctl launch --config deploy/fly/fly.toml --no-deploy
flyctl secrets set --app <name> AF_STACK_DATABASE_URL=... AF_STACK_KMS_KEY=...
flyctl deploy --config deploy/fly/fly.toml

# Railway
railway init --template ./deploy/railway/railway.json
railway up

# Render — Blueprint button or:
#   render.com -> New -> Blueprint -> point at deploy/render/render.yaml

# Compose
cp .env.example .env  # then edit
docker compose -f docker-compose.prod.yml up -d
```

## Common pitfalls

- **Postgres without pgvector.** AgentField's vector memory hard-fails on
  startup. Run `CREATE EXTENSION vector;` once, or use a host that
  preinstalls it (Render, Supabase, Neon).
- **Sandbox adapter = docker in prod.** The Docker socket gives the
  runtime root on the host. Use `e2b` (hosted) or `gvisor` (on-box,
  9.2). Never expose `/var/run/docker.sock` to a multi-tenant runtime.
- **Reusing `AF_STACK_AUTH_SECRET` across stacks.** Sessions issued by
  one stack are valid on the other. Each stack should generate its own.
- **Rotating KMS material without re-encrypting.** With
  `AF_STACK_KMS_PROVIDER=env`, changing `AF_STACK_KMS_KEY` makes existing
  rows unreadable. With cloud BYOK, changing the encrypted data key has
  the same effect unless it unwraps to the same 32-byte data key. Rotate
  by re-encrypting rows or by rewrapping the same data key under the new
  cloud KMS key.
- **External Postgres connection limits.** Run pgbouncer (or use Neon's
  built-in pooler) if you scale runtime replicas past 4. The runtime
  opens up to 25 connections per replica by default.
- **Caddy TLS on the first boot.** Let's Encrypt rate-limits to 5
  certs/week per domain. If you redeploy 6+ times during setup, point
  Caddy at the staging endpoint via `CADDY_ACME_CA=https://acme-staging-v02.api.letsencrypt.org/directory`.

## Backup & restore

See [docs/backup-restore.md](backup-restore.md) for:

- Postgres `pg_dump` cadence + retention.
- S3 bucket versioning + cross-region replication.
- Vault re-encryption procedure (KMS key rotation).
- Disaster-recovery drill checklist.

## Observability

Every target exposes `/health`, `/ready`, `/metrics` (Prometheus). The
compose Caddy gates `/metrics` behind `METRICS_ALLOW`. Kubernetes uses
the Helm chart's `ServiceMonitor`. Fly auto-discovers via the
`[[metrics]]` block in `fly.toml`. Render + Railway expose metrics via
their own collectors — no extra wiring needed.

## Links

- Architecture: [STACK.md](../STACK.md), [PRODUCT.md](../PRODUCT.md), [ARCHITECTURE.md](../ARCHITECTURE.md)
- Sandbox adapters trade-offs: [docs/sandbox-adapters.md](sandbox-adapters.md)
- Multi-tenancy model: [docs/multi-tenancy.md](multi-tenancy.md)
- Backup + restore: [docs/backup-restore.md](backup-restore.md)
