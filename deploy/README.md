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
