# AF Stack Helm chart

Production-grade Helm chart for [BackAI](https://github.com/Agent-Field/backai):
a single Go binary runtime plus a Next.js operator dashboard, wired to
Postgres + S3, with HPA, PDB, NetworkPolicy, and an optional Prometheus
ServiceMonitor.

| | |
| --- | --- |
| Chart version | `0.1.0` |
| App version | `0.1.0` |
| Kubernetes | `>=1.25` (preStop `sleep` action requires 1.29+) |
| Dependencies | `bitnami/postgresql` (optional), `bitnami/minio` (optional) |

## TL;DR

```bash
# 1. Fetch sub-chart dependencies (postgresql, minio).
helm dep update deploy/helm/af-stack

# 2. Lint.
helm lint deploy/helm/af-stack

# 3a. Dev install on k3d / kind:
helm install af-stack deploy/helm/af-stack -f deploy/helm/af-stack/values-dev.yaml

# 3b. Production install:
helm install af-stack deploy/helm/af-stack \
  -f deploy/helm/af-stack/values-prod.yaml \
  -f my-secret-overrides.yaml \
  --namespace af-stack --create-namespace
```

## CI smoke test

The chart has a kind-based smoke test:

```bash
scripts/test-helm-kind.sh
```

The script creates a disposable kind cluster, optionally builds the
runtime and dashboard images from the current checkout, installs the
chart with `values-dev.yaml`, and probes the chart-owned runtime
endpoints: `/health` and `/ready`.

For fast checks without creating a cluster:

```bash
scripts/validate-deploy-targets.py
```

That runs `helm lint` and `helm template` when Helm is installed.

## Two presets

| Preset | File | What it does |
| --- | --- | --- |
| **dev** | `values-dev.yaml` | Single replica per workload, in-chart Postgres + MinIO (emptyDir), autoscaling/PDB/NetworkPolicy off, traefik ingress (`af-stack.localtest.me`). For k3d, kind, CI. |
| **prod** | `values-prod.yaml` | 3 runtime / 2 dashboard replicas, autoscaling on, external Postgres + S3 via Secret refs, cert-manager + nginx ingress, NetworkPolicy on, PDB on, ServiceMonitor on. |

You apply them with `-f`; both are committed to the chart so they double
as worked examples of the value structure.

## Architecture rendered by the chart

```
                  ┌────────────────────────────────────────────┐
                  │  Ingress (className=<nginx|traefik>)       │
                  │   /, /sign-in, /...    -> dashboard:3000   │
                  │   /api, /openapi.json, /health, /ready     │
                  │                         -> runtime:8080    │
                  └────────────────────────────────────────────┘
                         │                            │
                    ┌────▼──────┐               ┌─────▼──────┐
                    │ dashboard │  RUNTIME_URL  │  runtime   │
                    │  (Next.js)│ ────────────▶ │   (Go)     │
                    │  :3000    │               │ :8080      │
                    │           │               │ :9090 (otl)│
                    └────┬──────┘               └─────┬──────┘
                         │                            │
                         │   AF_STACK_DATABASE_URL    │
                         │   ┌────────────────────────┘
                         ▼   ▼
                  ┌──────────────────┐      ┌──────────────────┐
                  │     Postgres     │      │  S3 / MinIO      │
                  │  (in-chart or    │      │  (AF_STACK_S3_*) │
                  │   external)      │      │                  │
                  └──────────────────┘      └──────────────────┘
```

## Values reference (top level)

| Key | Default | Description |
| --- | --- | --- |
| `image.runtime.repository` | `ghcr.io/agent-field/af-stack-runtime` | Runtime image. |
| `image.dashboard.repository` | `ghcr.io/agent-field/af-stack-dashboard` | Dashboard image. |
| `image.*.tag` | `""` | Defaults to `.Chart.AppVersion`. |
| `replicaCount.runtime` | `2` | Ignored when `autoscaling.runtime.enabled=true`. |
| `replicaCount.dashboard` | `2` | Ignored when `autoscaling.dashboard.enabled=true`. |
| `autoscaling.runtime.enabled` | `true` | HPA on the runtime Deployment. |
| `autoscaling.runtime.{min,max}Replicas` | `2`, `10` | HPA bounds. |
| `autoscaling.runtime.targetCPUUtilization` | `70` | HPA CPU target. |
| `autoscaling.runtime.targetMemoryUtilization` | `80` | HPA memory target. |
| `autoscaling.dashboard.*` | off by default | Same shape, off by default. |
| `podDisruptionBudget.runtime.enabled` | `true` | PDB on the runtime workload. |
| `podDisruptionBudget.runtime.minAvailable` | `1` | Tighten to `2` in prod. |
| `service.runtime.type` | `ClusterIP` | Service type. |
| `service.runtime.httpPort` | `8080` | Public API port. |
| `service.runtime.metricsPort` | `9090` | Prometheus metrics port. |
| `service.dashboard.type` | `ClusterIP` | Service type. |
| `service.dashboard.port` | `3000` | Dashboard port. |
| `ingress.enabled` | `true` | Render the Ingress. |
| `ingress.className` | `nginx` | IngressClass; override to `traefik` for k3d. |
| `ingress.annotations` | cert-manager + nginx defaults | Falsy values are filtered out. |
| `ingress.hosts[].host` | `af-stack.local` | Hostname. |
| `ingress.hosts[].paths` | `/api`, `/openapi.json`, `/health`, `/ready` -> runtime, `/` -> dashboard | Path mappings. |
| `ingress.tls` | `[]` | Standard k8s TLS block. |

### Database

| Key | Default | Description |
| --- | --- | --- |
| `postgresql.enabled` | `false` | Deploy the bitnami/postgresql sub-chart in-cluster. |
| `postgresql.external.url` | `""` | Inline Postgres URL (rendered into a chart Secret). Use only for dev. |
| `postgresql.external.urlSecretRef.name` | `""` | **Preferred** — reference an existing Secret holding `AF_STACK_DATABASE_URL`. |
| `postgresql.external.urlSecretRef.key` | `AF_STACK_DATABASE_URL` | Key inside the Secret. |
| `postgresql.auth.*` | various | Forwarded to the bitnami sub-chart. |

### Storage

| Key | Default | Description |
| --- | --- | --- |
| `storage.mode` | `s3` | `s3` (managed), `minio` (in-chart sub-chart), or `off` (disable blob endpoints). |
| `storage.s3.bucket` | `af-stack` | Bucket name. |
| `storage.s3.region` | `us-east-1` | Region. |
| `storage.s3.endpoint` | `""` | Empty -> AWS S3. Set for R2 / external MinIO. |
| `storage.s3.credentialsSecretRef.name` | `""` | **Preferred** — Secret with `AF_STACK_S3_ACCESS_KEY` + `AF_STACK_S3_SECRET_KEY`. |
| `storage.minio.enabled` | `false` | Deploy bitnami/minio sub-chart. |

### Modules (mirrors `apps/backend/config.yaml`)

| Key | Default |
| --- | --- |
| `modules.multiTenancy.enabled` | `true` |
| `modules.multiTenancy.strategy` | `pg-rls` |
| `modules.sandbox.enabled` | `false` |
| `modules.sandbox.adapter` | `noop` |
| `modules.llmGateway.enabled` | `true` |
| `modules.llmCache.enabled` | `false` |
| `modules.memory.enabled` | `true` |
| `modules.billing.enabled` | `false` |
| `modules.notifications.enabled` | `false` |
| `modules.webhooks.enabled` | `false` |

### Secrets

| Key | Default | Description |
| --- | --- | --- |
| `secrets.kmsKey` | `""` | KMS key for the secrets vault. Inline = renders chart Secret. |
| `secrets.kmsKeySecretRef.name` | `""` | **Preferred** — Secret holding `AF_STACK_KMS_KEY`. |
| `secrets.authSecret` | `""` | Dashboard session signing secret. |
| `secrets.authSecretSecretRef.name` | `""` | **Preferred** — Secret holding `AF_STACK_AUTH_SECRET`. |
| `llm.openrouterApiKey` etc. | `""` | Inline LLM keys (dev only). |
| `llm.providerSecretRef.name` | `""` | **Preferred** — one Secret with `OPENROUTER_API_KEY`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GOOGLE_API_KEY` (any subset). |

### Observability + networking

| Key | Default |
| --- | --- |
| `monitoring.serviceMonitor.enabled` | `false` |
| `monitoring.serviceMonitor.interval` | `30s` |
| `networkPolicy.enabled` | `true` |
| `networkPolicy.ingressControllerSelector.matchLabels.app.kubernetes.io/name` | `ingress-nginx` |
| `networkPolicy.ingressControllerNamespace` | `ingress-nginx` |

### Resources, scheduling, lifecycle

| Key | Default |
| --- | --- |
| `resources.runtime.requests` | `250m` / `512Mi` |
| `resources.runtime.limits` | `1` / `2Gi` |
| `resources.dashboard.requests` | `100m` / `256Mi` |
| `resources.dashboard.limits` | `500m` / `1Gi` |
| `nodeSelector` | `{}` |
| `tolerations` | `[]` |
| `affinity` | `{}` |
| `probes.runtime.liveness` | `/health`, 10s delay, 10s period |
| `probes.runtime.readiness` | `/ready`, 5s delay, 5s period, fail x3 |
| `terminationGracePeriodSeconds` | `60` |
| `preStopSleepSeconds` | `5` |

## Common operations

### Scale the runtime manually (autoscaler off)

```bash
helm upgrade af-stack deploy/helm/af-stack \
  -f deploy/helm/af-stack/values-prod.yaml \
  --set autoscaling.runtime.enabled=false \
  --set replicaCount.runtime=5
```

### Scale via the HPA

```bash
kubectl -n af-stack patch hpa af-stack-runtime \
  --type merge -p '{"spec":{"maxReplicas":20}}'
```

(Or update `values-prod.yaml` and re-`upgrade`.)

### Rotate the KMS key

**Automated rotation is not implemented.** The runtime loads one KEK at boot
and labels ciphertext with a fixed key id: there is no `af-stack secrets
rotate-kms`, nothing reads `AF_STACK_KMS_KEY_NEW`, and this chart ships no
rotate-kms CronJob. There is no dual-key window, so swapping the key before
you have exported the values makes every row in `suite_secrets` permanently
unrecoverable.

The only safe order is export → swap → re-write:

1. Back up the database.
2. **While the old key is still active**, read every secret out through the
   audited reveal endpoint (`POST /api/v1/vault/secrets/{key}/reveal` per
   tenant). The CLI has no reveal verb.
3. Generate the new key (`openssl rand -hex 32`), set `AF_STACK_KMS_KEY` to it
   in the Secret, and `helm upgrade` (or
   `kubectl rollout restart deploy/af-stack-runtime`) to pick it up. The vault
   is unreadable from here until step 4 finishes.
4. Re-write each value with `af-stack secrets set <key> --value-stdin`.
5. Archive the old key material — without it, anything missed in step 2 is gone.

Full runbook: `docs/backup-restore.md`.

### Swap the storage adapter

```bash
# From in-chart MinIO to managed S3:
helm upgrade af-stack deploy/helm/af-stack \
  -f deploy/helm/af-stack/values-prod.yaml \
  --set storage.mode=s3 \
  --set storage.minio.enabled=false \
  --set storage.s3.bucket=af-stack-prod \
  --set storage.s3.credentialsSecretRef.name=af-stack-s3
```

You'll need to copy existing objects between buckets out-of-band — the chart
doesn't migrate data.

### Tail logs across pods

```bash
kubectl -n af-stack logs -f -l \
  app.kubernetes.io/instance=af-stack,app.kubernetes.io/component=runtime \
  --tail=200 --max-log-requests=10
```

### Upgrade procedure

```bash
# 1. Pull the new chart version (or git pull this repo).
# 2. Re-resolve dependencies if the Chart.yaml deps changed.
helm dep update deploy/helm/af-stack

# 3. Diff first (requires the helm-diff plugin).
helm diff upgrade af-stack deploy/helm/af-stack \
  -f deploy/helm/af-stack/values-prod.yaml \
  -f my-secret-overrides.yaml

# 4. Apply.
helm upgrade af-stack deploy/helm/af-stack \
  -f deploy/helm/af-stack/values-prod.yaml \
  -f my-secret-overrides.yaml

# 5. Watch the rollout.
kubectl -n af-stack rollout status deploy/af-stack-runtime
kubectl -n af-stack rollout status deploy/af-stack-dashboard
```

The Deployment uses `maxUnavailable: 0, maxSurge: 1` and a 60s grace period,
so upgrades are zero-downtime as long as your HPA `minReplicas >= 2`.

## Known limitations

- **preStop `sleep` action requires Kubernetes 1.29+.** On older clusters the
  field is silently ignored; rely on the 60s `terminationGracePeriodSeconds` +
  the runtime's own SIGTERM handler to drain.
- **No Istio / Linkerd integration.** No sidecar annotations or
  ServiceEntry / TrafficSplit resources. Add manually if you run a mesh.
- **NetworkPolicy assumes ingress-nginx by default.** Override
  `networkPolicy.ingressControllerSelector` for traefik / contour / cilium.
- **KMS rotation is manual.** There is no rotate-kms job or CronJob; the
  supported path is export → swap → re-write (see above).
- **In-chart Postgres uses `emptyDir` in `values-dev.yaml`.** Data is lost on
  pod restart. Production must use external Postgres.
- **No PersistentVolumeClaims on the runtime.** It is stateless by design.
  If you flip a workload-module that needs disk, wire its own PVC outside
  this chart.
- **Single ingress host.** The default Ingress has one host with mixed paths.
  Multiple hosts work (loop over `ingress.hosts`) but TLS still needs one
  Secret per host.

## Verification commands

```bash
# Lint.
helm lint deploy/helm/af-stack

# Render dev preset.
helm template af-stack deploy/helm/af-stack -f deploy/helm/af-stack/values-dev.yaml \
  > /tmp/dev.yaml
grep -c '^kind:' /tmp/dev.yaml    # expect >= 10

# Render prod preset.
helm template af-stack deploy/helm/af-stack -f deploy/helm/af-stack/values-prod.yaml \
  > /tmp/prod.yaml
grep -c '^kind:' /tmp/prod.yaml   # expect >= 14
```
