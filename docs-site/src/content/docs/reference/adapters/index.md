---
title: Adapters reference
description: Every backend adapter the runtime ships. One row per package under services/runtime/internal/*/adapters/.
sidebar:
  order: 0
---

Modules talk to backends through small adapter interfaces. Pick the adapter via `config.yaml` (or env override) per module — same module surface, different implementation.

| Module | Adapters | Pick via |
|---|---|---|
| [Sandbox](./sandbox/) | docker, gvisor, firecracker, e2b | `sandbox.adapter` or `AF_STACK_SANDBOX_ADAPTER` |
| [Storage](./storage/) | minio, s3 | `storage.adapter` or `AF_STACK_S3_ADAPTER` |
| [Notifications](./notifications/) | log, resend | `modules.adapters.notifications` |
| [MCP](./mcp/) | stdio, sse | per-server (`transport` field on `suite_mcp_servers`) |

## Related

- [Modules reference](../modules/) — what the adapters plug into.
- [Hooks reference](../hooks/) — cross-cutting hook points.
