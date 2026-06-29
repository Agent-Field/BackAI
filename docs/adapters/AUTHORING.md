# Authoring a BackAI Adapter

> Walk a developer (you, your team, or a third-party startup) from
> "I want to plug my service into BackAI" to a working adapter in
> a few hours.

## TL;DR

1. Pick a **slot** (`sandbox`, `storage`, `notifications`, `secrets`,
   `billing`, or `multimodal`).
2. Read the **universal contract** ([`PROTOCOL.md`](PROTOCOL.md)) and
   the **per-slot specification** (`protocols/<slot>-v1.md`).
3. Implement the HTTP protocol in **any language**. The protocol is
   JSON over HTTP/1.1 with SSE for streaming endpoints.
4. Run the **conformance harness**:
   `backai-adapter-conformance --slot <slot> --url http://localhost:PORT`
5. Ship a container image. Operators plug you in by setting env vars:

   ```
   AF_STACK_<SLOT>_ADAPTER=remote
   AF_STACK_<SLOT>_ADAPTER_URL=http://your-sidecar:port
   AF_STACK_<SLOT>_ADAPTER_TOKEN=<optional-bearer>
   ```

No BackAI code changes. The runtime's generic remote-adapter shim
speaks the protocol to your sidecar.

---

## 1. Pick a slot

| Slot | What it does | Existing built-in adapters |
|---|---|---|
| `sandbox` | Run an OCI image + command in isolation, stream logs back | Docker, gVisor, Firecracker, e2b |
| `storage` | Object storage (upload, download, list, signed URLs) | MinIO, AWS S3 (and any S3-compatible) |
| `notifications` | Send transactional messages (email, SMS, Slack) | log, Resend |
| `secrets` | Store + reveal encrypted secrets | envelope-local |
| `billing` | Create customers, mint portal links, verify webhooks | Stripe, Lago |
| `multimodal` | LLM verbs that aren't pure chat: TTS, STT, image gen / edit | LiteLLM, ElevenLabs, Cartesia, Flux, fal |

Pick the one that matches what you want to provide. Each slot has its
own protocol spec; the universal contract applies to all of them.

If your service doesn't fit any slot, it's probably a **workload module**
or a **dashboard plugin** — see
`docs/ARCHITECTURE.md` §10.4 and §10.5.

---

## 2. Read the specs

Start with these in order:

1. [`PROTOCOL.md`](PROTOCOL.md) — what every adapter shares:
   transport, auth, envelope headers, RFC 7807 errors, SSE format,
   versioning, the common endpoints (`/healthz`, `/v1/capabilities`,
   `/v1/info`).

2. `protocols/<slot>-v1.md` — the per-slot endpoints, request shapes,
   response shapes, and behavior notes. Every per-slot spec ends with
   a **Conformance checklist** — the exact things the harness will
   check.

3. The reference Python adapter at
   [`examples/adapters/sandbox-echo-py/`](../../examples/adapters/sandbox-echo-py/) —
   a working, minimal implementation that passes the harness. ~300
   lines of FastAPI. Read it for the shape.

---

## 3. Implement

You can write the adapter in any language that can serve HTTP. We've
seen examples in Python (FastAPI), Go, Node, Rust, Elixir. Per-slot
specs are deliberately small — most slots need 6–10 endpoints.

The minimum every adapter must implement:

- `GET /healthz` — liveness + readiness; returns `{status,
  uptime_seconds, dependencies}`.
- `GET /v1/capabilities` — declare what your adapter supports.
  Critical — the runtime and dashboard adapt to your declared
  capabilities, so under-declaring is fine, over-declaring will be
  caught by the conformance harness.
- `GET /v1/info` — optional operator metadata (admin UI link, docs
  link, support contact).

Then the per-slot endpoints. For each method:

- Validate the `Authorization: Bearer <token>` header.
- Read `X-BackAI-Request-Id`, `X-BackAI-Idempotency-Key`, and
  `X-BackAI-Tenant-Id` headers — log them for traceability.
- For writes, honour idempotency: same `X-BackAI-Idempotency-Key` +
  same body MUST return the same response. Cache for ≥10 minutes.
- On error, return non-2xx with an RFC 7807 problem body:

  ```json
  {
    "type": "...",
    "title": "...",
    "status": 404,
    "detail": "...",
    "code": "object_not_found",
    "request_id": "01HZ..."
  }
  ```

  The `code` is the machine-readable identifier. Per-slot specs list
  the codes you should use.

### Streaming endpoints

For SSE endpoints (`POST /v1/runs/stream` in sandbox), respond with
`Content-Type: text/event-stream`, emit each event as
`data: {"...":"..."}` followed by an empty line, and flush after every
event. End every stream with a `terminated` event carrying the final
state.

Disconnect handling: if the client closes the connection mid-stream,
cancel the underlying operation. Don't leak goroutines / threads /
processes.

### Capability declaration

Be honest. If your sandbox adapter doesn't support GPU, set
`supports_gpu: false`. The runtime won't route GPU-requiring specs to
you. If you over-declare and the harness asks you to do something you
can't, the test will fail.

The full capability shape for your slot is in the per-slot spec.

---

## 4. Run the conformance harness

The harness is a Go CLI that hits your adapter and verifies the
protocol contract.

```bash
# Build it from source
cd backai/
go build -o backai-adapter-conformance ./services/runtime/cmd/backai-adapter-conformance/

# Run against your local adapter
./backai-adapter-conformance \
  --slot sandbox \
  --url http://localhost:8090 \
  --token your-bearer-if-required
```

Output:

```
PASS  GET /healthz returns 200 with envelope
PASS  GET /v1/capabilities returns slot envelope
PASS  GET /v1/info returns metadata (or 404)
PASS  Unknown fields in responses do not break decoding
PASS  Required Authorization header path is reachable
PASS  POST /v1/runs with a tiny spec returns terminal result
PASS  DELETE /v1/runs/{unknown-id} is idempotent (204 or 404)
PASS  GET /v1/pool returns adapter stats

Conformance: 8 / 8 checks passed
All checks PASS.
```

Exit code is `0` on full pass, `1` if any check fails. CI-friendly.

Add this to your adapter's GitHub Actions / GitLab CI / etc. so every
commit verifies you haven't broken the contract.

---

## 5. Ship it

Build a container image:

```dockerfile
FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt main.py ./
RUN pip install --no-cache-dir -r requirements.txt
EXPOSE 8090
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8090"]
```

Push to a registry the operator can reach (Docker Hub, GHCR, ECR,
private registry). Document the env vars your image expects (typically
just your provider API key — your sidecar reads it; the BackAI runtime
doesn't need it).

Then the operator wires it in:

```yaml
# docker-compose.override.yml
services:
  my-sandbox-sidecar:
    image: yourorg/my-sandbox:1.0
    environment:
      MY_PROVIDER_API_KEY: ${MY_PROVIDER_API_KEY}
    ports:
      - "8090:8090"
```

```bash
# .env
AF_STACK_SANDBOX_ADAPTER=remote
AF_STACK_SANDBOX_ADAPTER_URL=http://my-sandbox-sidecar:8090
AF_STACK_SANDBOX_ADAPTER_TOKEN=optional-bearer-if-you-want-auth
```

`docker compose up`, and your adapter is now the active sandbox for
that BackAI deployment. The runtime probes your `/healthz` at boot,
fetches your `/v1/capabilities`, and surfaces you under **Setup →
Adapters** in the operator's dashboard.

---

## 6. Operating considerations

### Logging

- Log request IDs (`X-BackAI-Request-Id`) for cross-system tracing.
- Never log secret values, request bodies that contain PII, or
  tenant-scoped payloads in plaintext.
- Structured logs (JSON) are recommended; the runtime ships its own
  to OTel.

### Multi-tenancy

- The runtime sets `X-BackAI-Tenant-Id` per request.
- You MAY use this for audit / metrics tags.
- You MUST NOT enforce tenant isolation yourself — that's the
  runtime's job. Your adapter sees one tenant at a time; if you
  serve multiple BackAI installations, isolation is handled at the
  process / network layer.

### Rate limiting

- If your upstream throttles you, surface `429 + Retry-After` (in
  seconds) — the runtime backs off automatically.
- The runtime sends `X-BackAI-Idempotency-Key` so retries are safe.

### Versioning

- Path prefix is `/v1`. When you need a breaking change, serve `/v2`
  alongside `/v1` so operators can upgrade at their own pace.
- Add fields freely — they're forward-compatible by protocol rule.
- Remove or rename fields → new major version.

### Updates

- Adapters are independent of the BackAI runtime release cycle.
- An operator can pull a new version of your image and restart the
  sidecar without touching the runtime.

### Scaling

- Stateless adapters scale horizontally — put a load balancer in
  front, point `AF_STACK_<SLOT>_ADAPTER_URL` at the LB.
- Stateful adapters (cache, idempotency dedup) need session affinity
  or a shared backing store; document this in your README.

---

## 7. FAQ

**Does my adapter have to talk to a real upstream?**
No. Adapters that simulate / mock for testing are valid — see the
`sandbox-echo-py` reference. Just declare capabilities honestly.

**Can my adapter implement only some of the per-slot endpoints?**
Yes. Set the corresponding capability to `false`. The runtime won't
route disallowed verbs to you. The harness only tests endpoints whose
capability is `true`.

**Can my adapter serve multiple slots?**
In principle yes, but the protocol assumes one slot per URL. Operators
set one URL per slot. If you want to handle multiple slots, expose
them on different ports or paths and have operators configure each
slot independently.

**Does my adapter need a database?**
Depends on the slot. Stateless adapters (most multimodal, some
sandbox) need none. Stateful adapters (storage, secrets) need their
own persistence — the BackAI runtime doesn't share its database with
adapters.

**How do I auth between BackAI and my adapter?**
Bearer token via `Authorization` header. The operator sets
`AF_STACK_<SLOT>_ADAPTER_TOKEN`; the runtime passes it on every
request. For dev setups without auth, the operator sets
`AF_STACK_<SLOT>_ADAPTER_AUTH=none`.

**Where do I get the runtime version?**
Every adapter request includes `X-BackAI-Runtime-Version`. Useful for
your logs / metrics; you may also use it to gate behaviour across
runtime versions.

**Can I run the conformance harness in CI without the BackAI runtime?**
Yes — the harness is a standalone Go binary. It only needs to reach
your adapter on a network port. Build it once, ship the binary as a CI
artifact.

**Does my adapter need to be open source?**
No. The protocol is open; your implementation can be whatever license
you choose. BackAI is Apache 2.0; the operator's fork is theirs.

---

## 8. Next steps

- Read [`CONFORMANCE.md`](CONFORMANCE.md) for the conformance harness
  in depth.
- Read the per-slot spec for your target slot.
- Read [`PROTOCOL.md`](PROTOCOL.md) for the shared contract.
- Reference implementation: `examples/adapters/sandbox-echo-py/`.
- Operating questions: see `docs/ARCHITECTURE.md` §10.3.
