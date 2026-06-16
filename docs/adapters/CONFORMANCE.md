# Adapter Conformance Harness

> The standalone tool that verifies a BackAI adapter sidecar speaks
> the protocol correctly.

## What it does

`backai-adapter-conformance` is a Go binary. Pointed at an adapter
URL, it issues a series of HTTP probes that match the **Conformance
checklist** at the end of every per-slot protocol spec
(`docs/adapters/protocols/<slot>-v1.md`).

It checks:

- Common contract: `/healthz`, `/v1/capabilities`, `/v1/info`,
  envelope headers, RFC 7807 error shape.
- Per-slot endpoints: exists, correct shape, correct status codes,
  idempotency where required, capability honesty.

It does **not** check upstream behaviour (e.g., it doesn't actually
verify your sandbox really isolates containers — that's beyond the
contract). It checks that you speak the protocol.

## Build

```bash
git clone https://github.com/Agent-Field/backai
cd backai
go build -o backai-adapter-conformance \
  ./services/runtime/cmd/backai-adapter-conformance/
```

You now have a single static binary. Ship it to CI if you want.

## Run

```bash
./backai-adapter-conformance --slot sandbox --url http://localhost:8090
```

Flags:

| Flag | Default | Meaning |
|---|---|---|
| `--slot` | required | One of: `sandbox`, `storage`, `notifications`, `secrets`, `billing`, `multimodal`, `llm-chat`, `auth`, `logs` |
| `--url` | required | The adapter's base URL (e.g. `http://localhost:8090`) |
| `--token` | empty | Bearer token if the adapter requires one |
| `--quiet` | `false` | Suppress per-check output; print only summary |
| `--timeout` | `15s` | Per-check HTTP timeout |

## Exit codes

| Code | Meaning |
|---|---|
| 0 | All checks PASS |
| 1 | At least one check FAILed |
| 2 | Invalid flags or unreachable adapter |

CI integration:

```yaml
# .github/workflows/conformance.yml
jobs:
  conformance:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: docker run -d -p 8090:8090 --name adapter $IMAGE
      - run: |
          curl -L -o backai-adapter-conformance \
            https://github.com/Agent-Field/backai/releases/latest/download/backai-adapter-conformance-linux-amd64
          chmod +x backai-adapter-conformance
      - run: ./backai-adapter-conformance --slot sandbox --url http://localhost:8090
```

## Example output

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

Failed check example:

```
PASS  GET /healthz returns 200 with envelope
FAIL  GET /v1/capabilities returns slot envelope — adapter declared slot="sandboxes"; expected "sandbox"
PASS  GET /v1/info returns metadata (or 404)
...

Conformance: 7 / 8 checks passed
1 FAIL — adapter does not yet conform.
```

The `FAIL` line carries enough detail to know what's wrong. The
adapter author fixes the issue and re-runs.

## What the checks look like under the hood

The harness uses the same shared HTTP client the BackAI runtime uses
(`services/runtime/internal/adapters/remote/`). This means:

- If your adapter passes the harness, the runtime can talk to it.
- If a check fails because of how the runtime / harness encodes
  headers, the runtime would have the same problem — fix it, both
  pass.

The harness uses `MaxRetries: 0` so transient failures aren't masked.
A check that needs retries would be flaky in production too.

## Per-slot check expansion

Each per-slot spec ends with a **Conformance checklist** listing the
exact behaviours the harness verifies. As the protocol grows, the
harness grows in lockstep. Pin a specific BackAI version in your CI to
avoid surprise new checks; bump when you're ready.

## Common pitfalls

- **`code` field missing in error responses.** The protocol requires
  RFC 7807; the runtime falls back to a status-derived code if the
  field is missing, but conformance asks for an explicit code.
- **Idempotency not implemented.** The harness sends the same
  `X-BackAI-Idempotency-Key` on retries; if your adapter returns a
  different response or duplicates the underlying action, it fails.
- **Stream not terminated.** SSE endpoints (sandbox stream) must end
  with a `terminated` event. If your stream just closes the socket,
  callers can't tell success from disconnect.
- **Capability declared but not honoured.** If you set
  `supports_streaming: true` but `POST /v1/runs/stream` 404s, the
  check fails.
- **Slot mismatch.** `capabilities.slot` must equal the slot you're
  serving. Easy typo, easy fix.

## Running locally against the reference adapter

Sanity check the harness against the working reference Python adapter:

```bash
# Terminal 1 — run the reference adapter
cd examples/adapters/sandbox-echo-py
python3.12 -m venv .venv
.venv/bin/pip install -r requirements.txt
.venv/bin/uvicorn main:app --port 18090

# Terminal 2 — run the harness
./backai-adapter-conformance --slot sandbox --url http://localhost:18090
```

You should see 8/8 PASS. If you don't, the harness build is wrong, not
your adapter.

Logs reference adapter:

```bash
cd examples/adapters/logs-echo-py
python3.12 -m venv .venv
.venv/bin/pip install -r requirements.txt
.venv/bin/uvicorn main:app --port 18091

./backai-adapter-conformance --slot logs --url http://localhost:18091
```

Traces reference adapter:

```bash
cd examples/adapters/traces-echo-py
python3.12 -m venv .venv
.venv/bin/pip install -r requirements.txt
.venv/bin/uvicorn main:app --port 18092

./backai-adapter-conformance --slot traces --url http://localhost:18092
```

## Extending the harness

The harness lives in
`services/runtime/cmd/backai-adapter-conformance/main.go`. Each slot
has a `runXxxChecks` function with `r.require(name, fn)` calls. To add
a check:

1. Open the per-slot protocol spec and add it to the **Conformance
   checklist**.
2. Add a corresponding `r.require(...)` call in the harness.
3. Update this doc and the per-slot spec to mention the new check.
4. Rebuild the binary; existing adapters re-run and reveal whether
   the check is genuinely a protocol requirement or a debatable
   convention.

The harness is intentionally conservative: only check things the
protocol explicitly requires. Don't grow it into a quality-assurance
suite.
