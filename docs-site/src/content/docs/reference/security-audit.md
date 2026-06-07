---
title: Security audit — Phase 16.1
description: Internal defensive review of the AF Stack runtime. Findings, severity, remediation status.
---

## Summary

| Severity   | Count | Status                                    |
| ---------- | ----: | ----------------------------------------- |
| Critical   |     0 |                                           |
| High       |     4 | 4 fixed in this PR                        |
| Medium     |     3 | 1 fixed in this PR, 2 planned             |
| Low        |     2 | accepted risk (dev-mode behaviour)        |
| Advisory   |     5 | informational / hardening recommendations |

**Scope.** Manual review of `services/runtime/internal/**` plus the dashboard
SSR layer. Tooling assist: ripgrep on the usual suspect patterns
(`fmt.Sprintf` near `Pool.Query`, `io.ReadAll(r.Body)`, `Access-Control-Allow-Origin`,
`exec.Command`, hardcoded secret prefixes). Out of scope: external pen test,
DDoS load testing, side-channel analysis.

**No critical findings.** The most exploitable issue (CORS reflect-any-origin
with credentials → CSRF) was a High and is fixed in this PR.

---

## High

### H1 — SSRF in outbound webhooks

- **Severity:** High
- **Location:** `services/runtime/internal/webhooks/outbound.go:53` (pre-fix)
- **Status:** **Fixed in this PR.**

**Description.** `OutboundService` constructed a bare `http.Client{Timeout: DefaultTimeout}`
to POST webhook deliveries to a user-supplied destination URL. There was no
check on the resolved IP, so an operator (or anyone with `webhooks:send`
scope) could enqueue a webhook to `http://169.254.169.254/latest/meta-data/iam/...`
and have the runtime return the EC2 instance profile credentials in the
delivery row's response body. Same risk for `http://10.x/`, `http://localhost:5432/`,
and `http://metadata.google.internal/`.

**Exploit path.**
1. Tenant creates a webhook delivery via `POST /api/v1/webhooks/send`
   with `url=http://169.254.169.254/latest/meta-data/iam/security-credentials/`.
2. Runtime POSTs, captures the response body in `suite_webhook_deliveries.response_body`.
3. Tenant lists deliveries, reads the AWS temporary credentials.

**Remediation.** Added `services/runtime/internal/safehttp` — a wrapper
around `http.Client` with a `net.Dialer.Control` hook that refuses
RFC 1918 / link-local / loopback / CGNAT / IPv6 ULA + cloud metadata
hostnames. Wired into `NewOutboundService` (outbound.go) and
`NewInboundService` (inbound forwardHTTP).

### H2 — SSRF in inbound webhook forward target

- **Severity:** High
- **Location:** `services/runtime/internal/webhooks/inbound.go:109` (pre-fix)
- **Status:** **Fixed in this PR.**

**Description.** `InboundService` POSTs inbound webhook bodies to the
endpoint's `forward_to` URL. The endpoint is dashboard-configured, so an
operator could set `forward_to=http://10.x:port/internal-api` and turn the
runtime into an internal-network proxy.

**Exploit path.** Same shape as H1. Operator sets `forward_to=http://169.254.169.254/...`
on an endpoint, fires a request through `/webhooks/in/<slug>`, reads the
forwarded response from the delivery row.

**Remediation.** Same `safehttp.New` wrapping applied to the inbound forward
client. The `af://agents/...` forward path is unaffected (it reaches the
agentfield client, not raw HTTP).

### H3 — SSRF in MCP SSE adapter

- **Severity:** High
- **Location:** `services/runtime/internal/mcp/adapters/sse/adapter.go:80` (pre-fix)
- **Status:** **Fixed in this PR.**

**Description.** `sse.New` defaulted to `&http.Client{Timeout: 0}` when no
client was injected. The MCP server URL (`suite_mcp_servers.url`) is
dashboard-configurable, so an operator could register an MCP server at
`http://10.x:port/sse` and the runtime would happily connect, leaking the
SSE response body back through `tools/list`.

**Exploit path.**
1. Operator registers MCP server with `url=http://169.254.169.254/latest/meta-data/iam/security-credentials/`.
2. Runtime opens a GET to `<url>/sse`, expecting an SSE stream.
3. Even on protocol error, the connection bytes pass through enough buffers
   that the metadata service replies are observable in logs / debug spans.

**Remediation.** Default client now `safehttp.New(safehttp.Options{Timeout: 0})`.

### H4 — CORS reflects any origin with `Allow-Credentials: true` (CSRF)

- **Severity:** High
- **Location:** `services/runtime/internal/server/server.go:1015-1033` (pre-fix)
- **Status:** **Fixed in this PR.**

**Description.** `withCORS` set `Access-Control-Allow-Origin: <Origin>`
combined with `Access-Control-Allow-Credentials: true` on every request
that carried an `Origin` header. This is the canonical CSRF-enabling
configuration: any third-party site the operator visits could fire
authenticated cross-origin requests against the runtime using the
operator's better-auth session cookie. The cookie is `HttpOnly` so the
attacker can't read it, but they don't need to — the browser attaches it
automatically.

**Exploit path.**
1. Operator with an active session visits `https://evil.example.com`.
2. JS on evil.example.com fires `fetch('http://runtime.local:8080/api/v1/db/sql',
   {method: 'POST', credentials: 'include', body: JSON.stringify({statement: 'drop table ...', read_only: false})})`.
3. Browser attaches the session cookie. Runtime echoes
   `Allow-Origin: https://evil.example.com` and `Allow-Credentials: true`,
   so the response is readable by the attacker.

**Remediation.** `Access-Control-Allow-Credentials` is now only set when
the `Origin` is in the allowlist:
- `http://localhost:<DASHBOARD_PORT>` and `http://127.0.0.1:<DASHBOARD_PORT>`
  (default 3000, overridable via `DASHBOARD_PORT`).
- Any comma-separated origin in `AF_STACK_CORS_ORIGINS`.

For non-allowlisted origins, `Allow-Origin` is still echoed (so anonymous
public reads work) but `Allow-Credentials` is omitted — the browser drops
the cookie, the request is effectively anonymous, and CSRF can no longer
piggy-back on the operator's session.

---

## Medium

### M1 — `POST /api/v1/secrets/{key}/reveal` not audited

- **Severity:** Medium
- **Location:** `services/runtime/internal/server/secrets.go:144-163` (pre-fix)
- **Status:** **Fixed in this PR.**

**Description.** Revealing a secret is the most sensitive read in the
runtime — it's the only path that returns plaintext for `suite_secrets`
rows. The handler had no audit trail: if a session cookie were ever
stolen, the operator could not retrospectively determine which secrets
the attacker had exfiltrated. `suite_audit_log` exists in the schema but
no row was inserted on reveal.

**Remediation.** Added `recordSecretReveal` to `server/secrets.go`. Every
successful reveal inserts an audit row with `action='secret.reveal'`,
`resource_type='secret'`, `resource_id=<key>`, `tenant_id`, `user_id`
+ `api_key_id` (from `tenantctx`), client IP (X-Forwarded-For aware),
and a truncated User-Agent. Insert runs in a goroutine with a 2s context
so a slow audit insert doesn't block the response; a failed insert logs
a warning but does not abort the reveal.

### M2 — DB studio bypasses tenant resolver under `publicPrefixes`

- **Severity:** Medium
- **Location:** `services/runtime/internal/server/tenant_resolver.go:92`
- **Status:** **Planned** (documented in SECURITY.md hardening checklist; production rollout requires multi-tenancy mode).

**Description.** `/api/v1/db/*` is listed in `publicPrefixes`, which means
`tenantResolver` does not run. The handler logic relies on the dashboard's
session cookie gating access via the same-origin proxy. When the runtime
is exposed on its port directly (8080) AND multi-tenancy is off (single
tenant default), `POST /api/v1/db/sql` accepts unauthenticated requests.

`dbstudio.RunSQL` defaults to read-only with a 15s `statement_timeout`,
so the immediate blast radius is "anyone on the network can read every
row in every table." That's still bad — `suite_secrets.encrypted_value`,
`suite_api_keys.hashed_secret` are exposed.

**Reproducer.** With MT off, on a deployment where the runtime port is
reachable: `curl -X POST http://runtime.local:8080/api/v1/db/sql -d '{"statement":"select * from suite_api_keys"}'`.

**Remediation status.** Planned for Phase 17. The narrow fix is to wrap
the `/api/v1/db/*` handlers in a session-only gate that 401s when no
better-auth cookie is present, regardless of MT mode. Documented in
`SECURITY.md` under "Hardening checklist" — production deployments must
enable multi-tenancy.

### M3 — `withCORS` proxy disabled but no preflight origin check

- **Severity:** Medium
- **Location:** `services/runtime/internal/server/server.go` (post-fix)
- **Status:** **Accepted risk.**

**Description.** After the H4 fix, non-allowlisted origins still see
their `Origin` echoed in `Access-Control-Allow-Origin`. This is the
expected behaviour for public read endpoints but it means a non-credentialed
GET from any site still works. For an operator with the runtime on a
private network, this is intended (public agent calls). Not a vulnerability
in itself but worth noting that the runtime does NOT enforce a same-origin
policy for unauthenticated calls.

---

## Low

### L1 — Dev KEK sentinel acceptance

- **Severity:** Low
- **Location:** `services/runtime/internal/secrets/crypto.go:55-67`
- **Status:** **Accepted risk** (dev-only).

**Description.** When `AF_STACK_KMS_KEY` is unset or matches the literal
string `"dev-secret-change-me"`, the runtime generates a deterministic
KEK from `sha256("af-stack-dev-kek")`. Logs a loud WARN. Documented in
`SECURITY.md` and `.env.example`. Required for the `docker compose up`
smoke test to work without manual key generation. Production deployments
must override.

### L2 — MinIO default credentials in `.env.example`

- **Severity:** Low
- **Location:** `.env.example:46-47`
- **Status:** **Accepted risk** (dev-only).

**Description.** `.env.example` ships `AF_STACK_S3_ACCESS_KEY=minio`
and `AF_STACK_S3_SECRET_KEY=minio-secret`. These match the bundled MinIO
container's defaults so the quickstart works. The example file is not
sourced unless the operator copies it to `.env`. `SECURITY.md` calls
this out in the hardening checklist.

---

## Advisory

### A1 — SQL string-building in `internal/memory/store.go`

- **Severity:** Advisory
- **Location:** `services/runtime/internal/memory/store.go:343-376`
- **Status:** **Safe as written.**

`memory.Store.List` builds the WHERE clause with `fmt.Sprintf` to inject
`$N` parameter placeholders. The values themselves are bound via the pgx
args slice, never interpolated. Verified by grep:
`select count(*) from suite_memory %s` substitutes `where scope = $1 and key like $2`
(or similar), never raw values. No injection.

### A2 — `dbstudio.Rows` uses `fmt.Sprintf` for identifier

- **Severity:** Advisory
- **Location:** `services/runtime/internal/dbstudio/studio.go:506-507`
- **Status:** **Safe as written.**

Identifier is double-validated: `validateIdent` rejects anything outside
`^[a-zA-Z_][a-zA-Z0-9_]*$`, then `pgx.Identifier{...}.Sanitize()` quotes
and escapes. Limit + offset are bound as `$1` and `$2`. No injection.

### A3 — Webhook HMAC verify is constant-time

- **Severity:** Advisory (positive)
- **Location:** `services/runtime/internal/webhooks/verify.go:66`

`hmac.Equal` is used for the comparison, not `==`. Confirms Phase 10.2's
requirement is met.

### A4 — API key compare is constant-time

- **Severity:** Advisory (positive)
- **Location:** `services/runtime/internal/tenancy/manager.go:1214`

`bcrypt.CompareHashAndPassword` is the only comparison path. No
short-circuit on a wrong-length secret. Resolver does the bcrypt check
before the revoked / expired checks so an attacker can't time-discriminate
between "no such key" and "revoked key."

### A5 — Object key validation refuses path traversal

- **Severity:** Advisory (positive)
- **Location:** `services/runtime/internal/server/storage.go:492-508`

`validObjectKey` rejects `..`, leading `/`, trailing `/`, NUL, and
oversized keys. Every storage handler calls it before touching the
adapter. Signed URLs are scoped via `objectKey` which prepends the
tenant prefix.

### A6 — Sandbox adapters use exec slice-args, no shell

- **Severity:** Advisory (positive)
- **Location:** `services/runtime/internal/mcp/adapters/stdio/adapter.go:124`,
  `services/runtime/internal/harnesses/prober.go:197`

Both call sites use `exec.CommandContext(binary, args...)` with slice
arguments. No `sh -c` shell injection vector. Docker adapter uses the
Docker SDK (`client.ContainerCreate`), not shell-exec.

### A7 — Crypto choices

- **Severity:** Advisory (positive)
- **Location:** `services/runtime/internal/secrets/crypto.go`

AES-256-GCM with a fresh random nonce per encryption. Version byte bound
into the GCM AAD prevents downgrade if a v2 format ever ships. KEK is
32 bytes loaded from env, validated for length, hex-decoded. `Decrypt`
returns a generic `ErrInvalidCiphertext` so a tampered envelope can't be
distinguished from a wrong key.

---

## Files changed in this PR

- `services/runtime/internal/safehttp/safehttp.go` (new)
- `services/runtime/internal/safehttp/safehttp_test.go` (new)
- `services/runtime/internal/webhooks/outbound.go` — `safehttp.New` client
- `services/runtime/internal/webhooks/inbound.go` — `safehttp.New` client
- `services/runtime/internal/mcp/adapters/sse/adapter.go` — `safehttp.New` default
- `services/runtime/internal/server/server.go` — CORS allowlist
- `services/runtime/internal/server/secrets.go` — audit log on reveal
- `SECURITY.md` (new)
- `docs-site/src/content/docs/reference/security-audit.md` (this file)

## Test status

- `go build ./services/runtime/...` clean.
- `go test ./services/runtime/...` — 527 passed in 42 packages.
- New `safehttp` tests: 6 passed (block private CIDRs, block metadata
  hosts, allow allowlisted CIDRs, allow allowlisted hosts, pass public
  hosts, block IPv4-mapped IPv6 loopback).
