# Audit — Blocks 2 & 3 of execution-blocks-v1.md

Verdict: **WRONG** (code contradicts), **GAP** (aspirational, work hidden), **OK**.

## Block 2 — `logs` adapter slot

### 2.1 Ring as default builtin
`logger/ring.go` `Ring` (60–101) exposes only `Append(Line)` (84) and `Recent(limit) []Line` (107). **No Subscribe / channel API.** `Query` via `Recent` is trivial; `Tail(ctx) (<-chan Entry, error)` requires building a subscriber registry — either extending `Ring` with fan-out, or intercepting at the slog handler (`ringHandler.Handle`, 169). Doc waves at this (line 243) without addressing backpressure. **GAP.**

### 2.2 "2048 lines runtime-process-only"
`logger/ring.go:27` — `const RingSize = 2048`. `server/logs.go:39, 46–59` reads via `s.logRing.Recent(limit)` clamped `[1,1000]`. **OK.**

### 2.3 `logs.Store` interface vs existing handler
Existing `server/logs.go:22–35, 60–73` emits `{ts, level, service, msg, request_id, tenant_id, agent, fields}` — matches `LogLineSchema` in `apps/dashboard/src/lib/api.ts`. Doc's `Entry` (execution-blocks-v1.md:184–194): `{ts, level, service, message, tenant_id, request_id, trace_id, fields}`.

- **`message` vs `msg`** — schema break; dashboard zod will reject unless remapped at wire.
- **Drops `agent`** — populated from slog attr (`ring.go:237`). Verbatim adoption loses it.
- `trace_id` new — won't populate until slog handler (`ring.go:227–247`) is extended.

**GAP** — workable with field-mapping at boundary; doc doesn't acknowledge.

### 2.4 LogQL translation (`filterToLogQL`)
Doc generates `{service=~"a|b|c", tenant_id="...", trace_id="..."} |~ "search" | level=~"warn|error"`.

- `{service=~"a|b|c"}` — OK *if* operators index `service` as a stream label.
- `|~ "search"` — **regex** line filter. Ring buffer does substring (doc claims `SupportsFullText: true` at 247). Use `|=` for parity, or `regexp.QuoteMeta` the input.
- `| level=~"warn|error"` — **WRONG in default deployments.** Post-pipeline label filter; only matches if `level` is extracted via parser (`| json`, `| logfmt`). Default Promtail/Alloy Docker configs do **not** promote `level`. Either inject `| json` before the level filter, or put `level` in the stream selector.
- `trace_id`/`tenant_id` as stream selectors — high-cardinality, Loki best-practice violation. Should be parser-extracted.

**WRONG** — syntactically valid LogQL that returns zero data on a vanilla Loki.

### 2.5 Loki tail endpoint
`/loki/api/v1/tail` is **WebSocket** — verified. **OK**, but needs WS client dep (`gorilla/websocket` / `nhooyr.io/websocket`) — not in `go.mod`. Affects the 2-day estimate.

### 2.6 Capabilities probe
Loki's `GET /config` returns YAML with `limits_config.retention_period`. Multi-tenant retention lives in per-tenant override files (OSS) or `/loki/api/v1/limits` (Enterprise). Doc's `probeRetention()` underestimates this. **GAP.**

### 2.7 Vector recommended shipper
Vector's `docker_logs` reads `/var/run/docker.sock` — **OK.** But Grafana's current Loki shipper is **Alloy** (Promtail entered LTS-only in 2024). Alloy deserves a mention.

---

## Block 3 — `traces` adapter slot

### 3.1 "OTel SDK wired, nothing receives spans"
`observability/observability.go:85–102` builds OTLP gRPC exporter when `endpoint != ""`. Sourced from `cfg.Observability.OTLPEndpoint`, populated at `config/config.go:208–210` from `OTEL_EXPORTER_OTLP_ENDPOINT`. **Env var IS honored — OK.** Doc's claim is shorthand for "no admin-facing reader."

### 3.2 No conflict with `/agentfield`
`server/run_agentfield.go:13` exposes `GET /api/v1/runs/{id}/agentfield` — AgentField reasoning-engine overview, unrelated to OTel traces. **OK.**

### 3.3 Tempo endpoints
- `GET /api/search?tags=` — **WRONG format.** Tempo accepts `tags` as a single **logfmt-encoded string**: `tags=service.name%3Dmyservice%20http.method%3DGET`. Not separate query params. Implementer must logfmt-encode the `Tag map[string]string`.
- `GET /api/v2/search?q=<TraceQL>` — **OK.** Stable since Tempo 2.0.
- `GET /api/traces/{traceID}` — **OK** as default. Tempo 2.4+ added `/api/v2/traces/{traceID}` (OTLP-shaped JSON). v1 returns legacy `batches[].resource_spans[]` — decoder must handle that shape.

### 3.4 Tempo on MinIO
Tempo's `backend: s3` works with gotchas: `s3.endpoint`, `s3.insecure: true` for plain-HTTP, `s3.forcepathstyle: true`. **GAP** — operator-deployment concern per locked decision #2.

### 3.5 `ParentSpanID` as `string`
OTel span IDs are 16-char lowercase hex. Root spans emit `""` (legacy Tempo JSON) or `"0000000000000000"` (OTLP). Adapter must normalize. **GAP** — minor decode quirk.

---

## Common to both blocks

### Env var naming — **WRONG by convention**
`config/config.go:240–263` and `cmd/af-stack/main.go:183, 212, 278, 305, 328, 346, 423` show uniform **`AF_STACK_<SLOT>_ADAPTER`**: `AF_STACK_S3_ADAPTER`, `AF_STACK_SANDBOX_ADAPTER`, `AF_STACK_NOTIFICATIONS_ADAPTER`, `AF_STACK_BILLING_ADAPTER`, `AF_STACK_AUTH_ADAPTER`, `AF_STACK_SECRETS_ADAPTER`. Doc proposes **`AF_STACK_LOGS_BACKEND`** (line 294), **`AF_STACK_TRACES_BACKEND`** (line 496). Should be `_ADAPTER` for `swap_env` field uniformity. (Backend-specific URLs like `AF_STACK_LOGS_LOKI_URL` are fine — not selection.)

### Wiring pattern
`cmd/af-stack/main.go:175–431` builds adapter from typed `cfg.<slot>.Adapter`, then `r.Register(adapterregistry.Slot{...})`. Doc §2.5's `switch os.Getenv(...)` reads env directly in `main.go` — shape-consistent but bypasses typed config. Should route through `internal/config`. **GAP.**

### Pre-registering before adapter exists
`adapters/registry/registry.go:160–177`: `Register(Slot)` is unconditional append. Idiom for "wired but unconfigured" is `Kind: KindNone, Status: StatusUnhealthy` (sandbox `main.go:188–214`, billing `292–314`). A `logs: ring (builtin)` row coexisting with future Loki is fine. **OK.**

---

## Summary

| Claim | Verdict |
|---|---|
| 2.1 Ring reusable as Store | GAP — Tail needs new Subscribe |
| 2.2 2048-line, process-only | OK |
| 2.3 Interface vs handler | GAP — `msg`/`message`, drops `agent` |
| 2.4 LogQL translation | **WRONG** — regex line filter, `level=~` needs parser |
| 2.5 Loki tail WebSocket | OK — new WS dep |
| 2.6 `/config` retention probe | GAP — YAML, multi-tenant |
| 2.7 Vector recommended | OK — Alloy is Grafana's pick |
| 3.1 OTel SDK present | OK |
| 3.2 No `/agentfield` conflict | OK |
| 3.3 Tempo endpoints | **WRONG** — `tags=` is logfmt string |
| 3.4 Tempo on MinIO | GAP — deferrable |
| 3.5 Span ID as string | GAP — root-span normalization |
| Env `_BACKEND` vs `_ADAPTER` | **WRONG** |
| Wiring via `os.Getenv` | GAP |
| Registry pre-registration | OK |

## Required fixes before implementing

**Block 2:**
1. `AF_STACK_LOGS_BACKEND` → `AF_STACK_LOGS_ADAPTER`.
2. LogQL: inject `| json` before `| level=~`, OR document indexed-label assumption.
3. Pick line-filter: `|=` (substring, ring parity) vs `|~` (regex). Document.
4. Add `Subscribe` to `logger.Ring` (or intercept in `ringHandler.Handle`) before Tail works on builtin.
5. Preserve `agent`; rename `Entry.Message` → `Msg` (or remap at wire) to match dashboard zod.
6. Route selection through `internal/config`, not `os.Getenv` in main.go.

**Block 3:**
1. Rename `AF_STACK_TRACES_BACKEND` → `AF_STACK_TRACES_ADAPTER`.
2. Fix Tempo `tags=` — single logfmt-encoded string.
3. Normalize root-span ParentSpanID (`""` ↔ `"0000000000000000"`).
4. Handle Tempo's legacy `batches[].resource_spans[]` shape (or pin `/api/v2/traces/{id}`).

Doc's overall shape (interface + builtin + Loki/Tempo + remote-shim + conformance) matches the existing 8-slot pattern. Bugs live at the translation and env-naming layers, not the architecture.
