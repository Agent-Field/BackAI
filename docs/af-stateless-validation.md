# Can AgentField run as a stateless single binary at scale?

Verdict: **yes, with caveats.** AF's control plane is already a stateless Go
binary IF you set `AGENTFIELD_STORAGE_MODE=postgres` and disable the
memory-fallback flags. The single-binary deploy story holds. There are a few
sharp edges to address before claiming "horizontally scalable" in marketing.

## What AF ships today

- **One Go binary**: `agentfield-server` (~43MB), embeds the React dashboard,
  serves REST + gRPC + UI from one process.
- **Three storage modes** (`AGENTFIELD_STORAGE_MODE`):
  - `local` — SQLite (`agentfield_local.db`) + BoltDB (`agentfield_local.bolt`)
    on local disk. **Not stateless.** Single-replica only.
  - `postgresql` — external Postgres for all persistence.
  - `cloud` — managed Postgres with replication tuning.
- **Agents register from anywhere** via REST + websocket. Each agent runs as a
  separate Python/Go/TS process. Agents are addressable by `node_id`.
- **Durable execution queue** lives in Postgres in `postgresql` mode
  (executions, workflows, identity, VCs all in PG).

## Where the statelessness claim holds

In `postgresql` mode:

- ✅ Agent execution records persist in PG (survive restarts)
- ✅ Workflow state machines persist in PG
- ✅ Verifiable credentials persist in PG
- ✅ Memory (KV + vector via pgvector) persists in PG
- ✅ Agent registrations persist in PG
- ✅ Embedded dashboard is static assets in the binary (stateless)
- ✅ Config can come from env vars, no file dependency
- ✅ Prometheus `/metrics` and structured logs are emit-only

You can run N control plane replicas behind a load balancer, all pointing at
the same Postgres, and serve any incoming agent call from any replica. This is
the desirable shape.

## Sharp edges to address

These don't break statelessness but need to be handled in the suite's deploy
guidance and in the AF runtime over time.

### 1. Memory-fallback flags default to `true`

The env defaults are concerning for multi-replica:

```
AGENTFIELD_STORAGE_POSTGRES_ENABLE_MEMORY_FALLBACK=true
AGENTFIELD_STORAGE_POSTGRES_ENABLE_DID_FALLBACK=true
AGENTFIELD_STORAGE_POSTGRES_ENABLE_VC_FALLBACK=true
```

When PG is unreachable, AF falls back to in-memory storage to keep serving.
On a single replica this prevents downtime. On N replicas this **causes
divergence** — each replica accumulates its own in-memory state, then
reconciliation is impossible.

**Suite default**: set all three fallbacks to `false` in the production compose
and helm chart. Force PG-only writes. Document this clearly. Fallbacks are for
single-replica dev mode only.

### 2. The k8s manifests include a 1Gi PVC

`deployments/kubernetes/base/control-plane-pvc.yaml` requests a
`PersistentVolumeClaim` with `ReadWriteOnce` access mode. This is a leftover
from local mode and **prevents N-replica scaling on standard k8s** (RWO volumes
mount to one node).

**Suite default**: helm chart should make the PVC conditional on storage mode.
With `postgresql` mode the PVC is unnecessary; the deployment scales freely.
With `local` mode the PVC is required and replicas must be 1.

### 3. Installation registry uses local filesystem

`internal/infrastructure/storage/registry.go` has `LocalRegistryStorage` that
reads/writes JSON files for "installed packages" (MCP servers, etc.). On a
multi-replica deployment, replica A could install a package, replica B
wouldn't see it until restart.

**Action**: verify that `postgresql` mode wires the registry into PG. If it
does (which the storage interface suggests is intended), good. If not, this is
a small AF PR to make registry storage backend-agnostic. Either way it's
fixable and doesn't block v1.

### 4. WebSocket connections from agents are sticky

Agent processes connect to the control plane over WebSocket for the agent's
callback URL. Each WebSocket connects to one specific replica. If that replica
dies, the agent reconnects to another. As long as no in-memory state about the
connection is required for routing, this works.

**Verify**: when control plane needs to call an agent, does it look up which
replica holds the WebSocket, or does any replica forward the call (via
agent's HTTP callback)?

The README implies HTTP-based callbacks: `AGENT_CALLBACK_URL=http://swe-agent:8003`.
That's an HTTP URL, not a WebSocket. Any control plane replica can hit it.
That's the right design and means **statelessness holds even with N replicas**.

### 5. Workflow DAG live updates

The dashboard streams workflow DAGs in real time. If the user is connected to
replica A and the execution updates land on replica B, replica A needs to know.

**Options:**
- PG `LISTEN/NOTIFY` for fan-out (simple, low-throughput-ok)
- NATS or Redis pub/sub for high throughput
- Poll the DB at short intervals (works, less crisp)

**Suite default**: PG LISTEN/NOTIFY for v1. Document NATS adapter for high-volume
production.

### 6. Sandbox host (when bundled by the suite) is stateful

This isn't AF's problem, but worth noting: the suite's sandbox host (Firecracker
pool or docker daemon) is inherently stateful. Each sandbox runs on one node.
At scale, the sandbox host becomes a dedicated tier separate from the control
plane. Standard pattern (k8s job, fly machines, e2b API).

## What this means for the suite

The suite can promise:

> Single binary. Stateless when configured with Postgres. Scales horizontally
> behind a load balancer.

…with these honest footnotes:

1. The "local" storage mode is for dev only. Production uses Postgres.
2. The default config disables memory-fallback flags (production-safe).
3. The helm chart drops the PVC in PG mode.
4. Sandbox host is a separate tier; the control plane stays stateless.

This story is **viral-grade** for self-host pitches. Caddy, Gitea, Plausible,
Grafana, MinIO all run single-binary + external-DB at massive scale.
AgentField is in that lineage.

## Sketch: production deploy at scale

```
                    ┌──────────────┐
                    │ Load balancer │
                    └──────┬───────┘
                           │
        ┌──────────────────┼──────────────────┐
        ▼                  ▼                  ▼
  ┌──────────┐       ┌──────────┐       ┌──────────┐
  │ AF + AF  │       │ AF + AF  │       │ AF + AF  │   ← N replicas of
  │ Stack    │       │ Stack    │       │ Stack    │     the single binary
  │ binary   │       │ binary   │       │ binary   │     (stateless)
  └──────────┘       └──────────┘       └──────────┘
        │                  │                  │
        └──────────────────┼──────────────────┘
                           ▼
                    ┌──────────────┐
                    │  Postgres    │   ← all durable state
                    │ (managed or  │     (executions, workflows, memory,
                    │  self-host)  │      identity, VCs, MT data, jobs)
                    └──────────────┘
                           │
                    ┌──────┴──────┐
                    ▼             ▼
              ┌──────────┐  ┌──────────┐
              │   S3     │  │ Sandbox  │   ← separate tiers
              │  / MinIO │  │ host pool│
              └──────────┘  └──────────┘
                            (Firecracker
                             / e2b / k8s)
```

Each AF+Stack binary serves any incoming request. Postgres holds all state.
Sandbox host is its own pool. S3 holds blobs. Standard 3-tier shape.

## Recommendation

Ship the suite's default deploy with:

- `AGENTFIELD_STORAGE_MODE=postgres`
- `AGENTFIELD_STORAGE_POSTGRES_ENABLE_MEMORY_FALLBACK=false`
- `AGENTFIELD_STORAGE_POSTGRES_ENABLE_DID_FALLBACK=false`
- `AGENTFIELD_STORAGE_POSTGRES_ENABLE_VC_FALLBACK=false`
- Helm chart: PVC conditional, default off in PG mode
- Document that `local` mode is dev-only and single-replica
- Verify registry storage is PG-backed in PG mode (one read of AF code, or a
  one-line AF PR if not)

With these settings the single-binary stateless claim is honest.
