# Backend Handoff — Ready for UI

> What the next agent (UI builder) needs to know about the backend
> that's been built. Everything below ships; tests pass; docs exist.

## What's done

### Adapter system (the modularity spine)

Five docs and one binary that together define **how every BackAI
subsystem becomes pluggable**:

- [`docs/adapters/PROTOCOL.md`](adapters/PROTOCOL.md) — universal HTTP
  contract every remote adapter speaks (auth, envelope, RFC 7807,
  SSE, versioning).
- [`docs/adapters/AUTHORING.md`](adapters/AUTHORING.md) — how to write
  your own adapter, in any language.
- [`docs/adapters/CONFORMANCE.md`](adapters/CONFORMANCE.md) — how to
  verify an adapter conforms.
- [`docs/adapters/protocols/<slot>-v1.md`](adapters/protocols/) — per-slot
  specs for sandbox, storage, notifications, secrets, billing,
  multimodal.
- `services/runtime/cmd/backai-adapter-conformance/main.go` — Go binary
  that runs the conformance suite against any adapter URL.

### Six remote-adapter shims (Go)

For every slot, a Go shim that satisfies the slot's interface by
speaking HTTP to a sidecar:

| Slot | Interface | Shim location | Tests |
|---|---|---|---|
| sandbox | `sandbox.Sandbox` | `services/runtime/internal/sandbox/adapters/remote/` | 11/11 unit + 1 E2E |
| storage | `storage.Storage` | `services/runtime/internal/storage/adapters/remote/` | 13/13 |
| notifications | `notifications.Adapter` | `services/runtime/internal/notifications/adapters/remote/` | 15/15 |
| secrets | `secrets.Store` (newly extracted) | `services/runtime/internal/secrets/adapters/remote/` | 13/13 |
| billing | `billing.Client` | `services/runtime/internal/billing/adapters/remote/` | 15/15 |
| multimodal | `MultimodalAdapter` | `services/runtime/internal/llmgateway/adapters/remote/` | 15/15 |

All shims share a single HTTP transport at
`services/runtime/internal/adapters/remote/` (21/21 tests):

- Authenticated `*http.Client` (bearer + connection pooling)
- RFC 7807 typed errors (`*Problem`, `IsCode`)
- SSE event parser (`Event`, streamed via `<-chan Event`)
- Idempotency-key auto-generation for writes
- Retry with jittered backoff on 5xx + 429 + transient network
- Capability cache (5min TTL)
- Context cancellation propagation end-to-end
- No buffering of large bodies; streaming uploads + downloads

### Adapter registry & introspection endpoint

`services/runtime/internal/adapters/registry/` (9/9 tests):

- `Registry` collects every wired slot at boot.
- `GET /api/v1/admin/adapters` returns the four-tier slot inventory
  defined in
  [`PROTOCOL.md`](adapters/PROTOCOL.md) §11.
- Per-slot health probes refreshed on a 30s TTL.

### Reference implementation

`examples/adapters/sandbox-echo-py/` — a working FastAPI adapter that
implements the sandbox-v1 protocol. ~300 lines. Passes the
conformance harness end-to-end (8/8 checks).

### Architecture documentation

[`docs/ARCHITECTURE.md`](ARCHITECTURE.md) — 19 sections covering:
8-band stack, repo layout, the 4-tier adapter system, three sequence
diagrams (mermaid) for chat completion / async sandbox / agent
invocation, multi-tenancy via Postgres RLS, data architecture,
concurrency model, deployment topology, how to extend (agents,
adapters, modules, plugins), testing strategy, middleware chain,
where agents fit, customer-app, dashboard, pitfalls, versioning,
glossary, onboarding path.

### Dashboard spec (already done by user)

`docs/dashboard/spec-v1.md` — page-by-page spec the UI builder
implements. 44 pages across Overview / Operate / Build / Customers /
Setup / Brand.

### E2E verification

Two E2E tests that actually launch dependencies:

- `services/runtime/internal/sandbox/adapters/remote/e2e_test.go`
  — launches the Python sandbox reference adapter as a child process,
  drives the full sandbox protocol through the Go shim. **PASSES**.
- `services/runtime/internal/llmgateway/e2e_openrouter_test.go`
  — makes a real OpenAI-compatible chat completion request against
  OpenRouter using `moonshotai/kimi-k2-0905`. **PASSES**.

Run with `go test -tags=e2e ./services/runtime/...`.

## Reasoning slot

The agent runtime is positioned as the **reasoning** slot (Tier 3 —
interface-swappable). The only adapter today is `agentfield`. Surfaced
via the existing `internal/agentfield/` client and the `/api/v1/agents/*`
HTTP surface. See `docs/ARCHITECTURE.md` §4.3.

## Test inventory

| Test type | Where | Count | Status |
|---|---|---|---|
| Unit + integration (httptest) | per-package `_test.go` | 112 across adapter packages | all pass |
| Vet | `go vet ./services/runtime/...` | n/a | clean |
| Build | `go build ./...` | n/a | clean |
| E2E sandbox | `e2e_test.go` (build-tag) | 1 | pass against real Python sidecar |
| E2E LLM | `e2e_openrouter_test.go` (build-tag) | 1 | pass against real OpenRouter+Kimi |
| Conformance | `cmd/backai-adapter-conformance/` | 8 per sandbox run | pass |
| Total packages | `go test ./services/runtime/...` | 59 | all pass |

## What the next agent needs to build (UI)

Per `docs/dashboard/spec-v1.md`:

1. Implement the 6 sidebar groups (Overview, Operate, Build,
   Customers, Setup) plus pinned Brand.
2. Wire each page to its documented runtime endpoint in
   `services/runtime/internal/server/server.go`.
3. The `Setup → Adapters` page is powered by
   `GET /api/v1/admin/adapters` (registry).
4. The capability pill on adapter-backed pages reads from the same
   registry response.
5. Three-state pattern (empty / missing / degraded) per spec §16.

## Quick verification before starting UI work

```bash
# From repo root
go build ./...
go vet ./services/runtime/...
go test ./services/runtime/...

# E2E (LLM E2E requires OPENROUTER_API_KEY in env)
OPENROUTER_API_KEY=$OPENROUTER_API_KEY \
  go test -tags=e2e ./services/runtime/internal/sandbox/adapters/remote/ \
                    ./services/runtime/internal/llmgateway/

# Build conformance binary
go build -o /tmp/backai-adapter-conformance \
  ./services/runtime/cmd/backai-adapter-conformance/

# Run conformance against the Python reference adapter
cd examples/adapters/sandbox-echo-py
python3.12 -m venv .venv  # one time
.venv/bin/pip install -r requirements.txt
.venv/bin/uvicorn main:app --port 18090 &
sleep 2
/tmp/backai-adapter-conformance --slot sandbox --url http://localhost:18090
# Expect: 8 / 8 checks passed
```

## Open items (intentionally deferred)

- Tier-3 adapter interfaces for auth / job queue / outbound webhooks
  are not extracted yet (current impls are still tightly coupled to
  better-auth / River / Svix). Documented as future work in
  [`PROTOCOL.md`](adapters/PROTOCOL.md) §14.
- Reasoning slot has only one adapter (`agentfield`). Future
  alternative reasoning adapters will follow the same shim pattern.
- The `/api/v1/admin/adapters` endpoint is implemented at the package
  level (registry + handler); the wire-up into the main HTTP mux
  happens in `services/runtime/cmd/af-stack/main.go` and is a small
  next-step glue change (~5 lines), not done in this pass to keep the
  wider runtime untouched.
