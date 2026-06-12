# Sandbox adapters

BackAI runs untrusted workloads through a `sandbox.Sandbox` interface
(see `services/runtime/internal/sandbox/interface.go`). BackAI ships four
adapters; pick the one whose isolation / cost / latency profile fits your
deployment.

## Selection

Set the adapter in `config.yaml`:

```yaml
sandbox:
  adapter: docker         # docker | gvisor | firecracker | e2b
  e2b_api_key: ""         # only when adapter=e2b
  e2b_base_url: ""        # optional; defaults to https://api.e2b.dev
```

Or via env (overrides YAML):

```bash
AF_STACK_SANDBOX_ADAPTER=gvisor
E2B_API_KEY=sk-...
AF_STACK_E2B_BASE_URL=https://api.e2b.dev
```

The runtime logs the selection at startup:

```
sandbox adapter ready  adapter=gvisor max_timeout_s=86400 ...
```

## Adapter matrix

| Adapter       | Isolation        | Cold start | Max timeout | GPU | Network | Mounts | Operate yourself? |
| ------------- | ---------------- | ---------- | ----------- | --- | ------- | ------ | ----------------- |
| `docker`      | container (runc) | ~500 ms    | 24h         | yes | yes     | yes    | yes               |
| `gvisor`      | userspace kernel | ~500 ms    | 24h         | yes | yes     | yes    | yes               |
| `firecracker` | micro-VM         | ~5 s       | 24h         | yes | yes     | yes    | yes (v2)          |
| `e2b`         | micro-VM (hosted)| ~1 s       | 1h          | no  | yes     | yes    | no                |

## When to pick which

### `docker` — local dev, trusted workloads

The default. Same Docker daemon that runs the rest of `docker-compose`.
No extra host setup, lowest cold start. **Do not use for multi-tenant
untrusted code in production** — a kernel CVE compromises the host.

### `gvisor` — production multi-tenant, untrusted workloads

Thin wrapper around the Docker adapter that forces
`HostConfig.Runtime = "runsc"`. gVisor is a userspace kernel that
intercepts container syscalls, dramatically reducing the kernel attack
surface. The trade-off is ~10–30% performance and occasional syscall
incompatibility (uncommon for typical app workloads).

**Install requirements:**

```bash
# 1. Install runsc on the Docker host
apt-get install runsc        # Debian/Ubuntu
# or: curl https://gvisor.dev/install.sh | bash

# 2. Register runsc as a Docker runtime in /etc/docker/daemon.json
{
  "runtimes": {
    "runsc": { "path": "/usr/bin/runsc" }
  }
}

# 3. Restart Docker
systemctl restart docker

# 4. Set sandbox.adapter = gvisor
```

Verify with `docker info | grep -i runsc` — `runsc` should appear under
`Runtimes`.

### `firecracker` — heavy isolation, you operate it (v2)

Firecracker runs each workload in its own KVM micro-VM. Stronger
isolation than gVisor, but Firecracker on its own does not orchestrate
VM lifecycle, networking, or pool warm-up across a fleet.

**The Firecracker adapter is a scaffold.** Calling `Run` /
`Stream` / `Stop` returns `ErrFirecrackerRequiresFlintlock`. The
production v2 deployment will pair this adapter with
[Flintlock](https://github.com/liquidmetal-dev/flintlock) (or an
equivalent orchestrator) that exposes a gRPC API for VM lifecycle. The
runtime keeps booting and reports the adapter as unavailable so
operators see the misconfiguration immediately.

Host requirements (when production wiring lands):

- Bare-metal or KVM-capable VM host (Linux x86_64 / aarch64)
- Firecracker binary >= v1.x in PATH
- Flintlock reachable for VM orchestration
- Published kernel + rootfs images

### `e2b` — hosted, no infra to operate

[e2b.dev](https://e2b.dev) runs Firecracker-based sandboxes you don't
operate. Each `Run` call is three HTTP requests against e2b's control
plane: create sandbox -> write files -> exec command -> close sandbox.
Logs come back inline on the exec response.

**Pick this when:**

- You want micro-VM isolation without operating Firecracker yourself.
- Your max workload runtime fits inside e2b's 1-hour limit.
- You're OK with per-run cost (e2b prices per sandbox-second).

Set `E2B_API_KEY` and `AF_STACK_SANDBOX_ADAPTER=e2b`. The runtime fails
fast at startup if the key is missing.

## Capabilities at runtime

Every adapter implements `Capabilities()` which returns the same shape
the dashboard `SandboxCapabilitiesSchema` expects. The runtime exposes
this on `GET /api/v1/sandbox/pool` so the operator UI can render the
adapter card, and so the runtime can reject runs whose `timeout_s`
exceeds the adapter's `max_timeout_s` before any work is queued.

## Adapter Status

| Adapter       | Status                                                                |
| ------------- | --------------------------------------------------------------------- |
| `docker`      | Local development implementation. |
| `gvisor`      | Thin Docker wrapper for stronger local isolation. |
| `firecracker` | Scaffold; production v2 should use Flintlock or a managed equivalent. |
| `e2b`         | Managed adapter; streaming uses buffered fallback until native stream support. |

## See also

- `services/runtime/internal/sandbox/interface.go` — the `Sandbox`
  interface and `RunSpec` / `RunResult` / `Capabilities` types.
- `apps/dashboard/src/lib/api.ts` — `SandboxCapabilitiesSchema` and
  `SandboxRunInputSchema` (the wire contract).
- Historical sandbox roadmap docs in `docs/archive/`.
