# BackAI Adapters

Everything you need to know to build, run, or contribute an adapter
to BackAI.

## Start here

| If you want to... | Read... |
|---|---|
| Understand the adapter system | [`../ARCHITECTURE.md`](../ARCHITECTURE.md) §4 |
| Build your own adapter | [`AUTHORING.md`](AUTHORING.md) |
| Verify an adapter conforms | [`CONFORMANCE.md`](CONFORMANCE.md) |
| Read the universal HTTP contract | [`PROTOCOL.md`](PROTOCOL.md) |

## Per-slot protocol specs

The HTTP contract every remote adapter speaks, per slot.

| Slot | Protocol | Operator config |
|---|---|---|
| **sandbox** — code execution | [`protocols/sandbox-v1.md`](protocols/sandbox-v1.md) | [`sandbox.md`](sandbox.md) |
| **storage** — object storage | [`protocols/storage-v1.md`](protocols/storage-v1.md) | [`storage.md`](storage.md) |
| **notifications** — email/sms/slack | [`protocols/notifications-v1.md`](protocols/notifications-v1.md) | [`notifications.md`](notifications.md) |
| **secrets** — encrypted vault | [`protocols/secrets-v1.md`](protocols/secrets-v1.md) | [`secrets.md`](secrets.md) |
| **billing** — stripe/lago | [`protocols/billing-v1.md`](protocols/billing-v1.md) | [`billing.md`](billing.md) |
| **multimodal** — TTS/STT/image | [`protocols/multimodal-v1.md`](protocols/multimodal-v1.md) | *(operator config in main env)* |
| **llm-chat** — OpenAI-compat chat + embeddings | [`protocols/llm-chat-v1.md`](protocols/llm-chat-v1.md) | *(via Setup → LLM providers)* |
| **auth** — session verification + OAuth | [`protocols/auth-v1.md`](protocols/auth-v1.md) | *(via Setup → Auth providers)* |

## Reference implementation

[`examples/adapters/sandbox-echo-py/`](../../examples/adapters/sandbox-echo-py/)
— a minimal, working FastAPI adapter that passes the conformance
harness. ~300 lines. Read it as a template.

## The conformance binary

```bash
go build -o backai-adapter-conformance \
  ./services/runtime/cmd/backai-adapter-conformance/

./backai-adapter-conformance --slot sandbox --url http://localhost:8090
```

See [`CONFORMANCE.md`](CONFORMANCE.md).

## The runtime side (Go)

Every slot's remote-adapter shim lives under
`services/runtime/internal/<slot>/adapters/remote/`. They share the
HTTP client at
`services/runtime/internal/adapters/remote/`. The shared client
handles auth headers, idempotency, RFC 7807 errors, retry policy,
capability caching, and SSE parsing.

## Naming and slot vocabulary

- **Slot** — an adapter-backed subsystem.
- **Adapter** — one implementation of a slot's interface.
- **Built-in adapter** — Go code in `<slot>/adapters/<name>/`.
- **Remote adapter** — a sidecar process speaking the per-slot HTTP
  protocol, plugged in via `AF_STACK_<SLOT>_ADAPTER=remote`.
- **Capability** — a flag in `/v1/capabilities` declaring what the
  active adapter supports.

The runtime exposes `GET /api/v1/admin/adapters` so the dashboard can
show what's plugged in per slot.
