# Deployment recipes

This directory ships ready-to-use deployment artifacts for AF Stack
across several runtimes. Pick the one that matches your target.

| Target | Path | Best for |
| --- | --- | --- |
| **Kubernetes (Helm)** | [`helm/af-stack/`](./helm/af-stack/) | Self-hosted clusters, EKS / GKE / AKS, k3d, kind. Production-ready: HPA, PDB, NetworkPolicy, optional ServiceMonitor. |
| Fly.io | [`fly/`](./fly/) | Single-region demos. `fly.toml` per app. |
| Railway | [`railway/`](./railway/) | One-click hosted deploy. |
| Render | [`render/`](./render/) | Hosted PaaS with managed Postgres. |
| Caddy | [`caddy/`](./caddy/) | Reverse-proxy / TLS sidecar for bring-your-own VM installs. |
| Nomad | [`nomad/`](./nomad/) | (placeholder — Nomad job specs forthcoming) |

For the canonical install path, see the [Helm chart README](./helm/af-stack/README.md).

## CI validation

Deploy targets are checked by the manual CI workflow and can be checked
locally:

```bash
scripts/validate-deploy-targets.py
```

That validates Helm lint/template output, Fly config structure, Railway
template JSON, Render Blueprint structure, and production compose syntax.

For the real Kubernetes smoke test, run:

```bash
scripts/test-helm-kind.sh
```

It creates a disposable kind cluster, installs the chart with
`values-dev.yaml`, then probes the runtime `/health` and `/ready`
endpoints. Cloud-provider end-to-end deploys still require provider
tokens and staging apps configured outside the repo.

Credential-gated CI smoke tests are also wired:

| Gate | Script | Required configuration |
| --- | --- | --- |
| Fly staging deploy | `scripts/test-fly-staging.sh` | `FLY_API_TOKEN` secret, `AF_STACK_FLY_STAGING_RUNTIME_APP` and `AF_STACK_FLY_STAGING_DASHBOARD_APP` repo variables |
| Production compose bring-up | `scripts/test-prod-compose-smoke.sh` | `AF_STACK_PROD_COMPOSE_SMOKE=true` repo variable plus external Postgres/S3/domain secrets documented in `docker-compose.prod.yml` |

When those values are absent, the jobs skip cleanly so forks can still
run CI before they have staging infrastructure.
