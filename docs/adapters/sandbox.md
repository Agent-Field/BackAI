# Sandbox Adapters

Sandbox adapters provide ephemeral compute for code execution and agent
actions. Activity is shown in `Operate -> Sandbox Activity`.

## Active selector

Set:

```bash
AF_STACK_SANDBOX_ADAPTER=docker
```

Supported today:

| Adapter | Use |
|---|---|
| `docker` | Local development and simple self-hosted deployments |
| `gvisor` | Stronger container isolation on compatible Linux hosts |
| `firecracker` | MicroVM path; requires host support |
| `e2b` | Managed sandbox provider; requires `E2B_API_KEY` |

Planned:

| Adapter | Notes |
|---|---|
| `modal` | Managed compute adapter |

## Provider env

```bash
E2B_API_KEY=
AF_STACK_E2B_BASE_URL=
```

See also [`docs/sandbox-adapters.md`](../sandbox-adapters.md).
