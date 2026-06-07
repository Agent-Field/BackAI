# Security policy

## Reporting a vulnerability

**Email `security@agentfield.ai`** (placeholder — replace with the live mailbox before public release).
Do not open a public GitHub issue for a suspected vulnerability.

- We acknowledge within **48 hours**.
- Coordinated disclosure window: **90 days** from acknowledgement.
- PGP key: TBD. For now, send a plain-text email and we will reply with a
  channel for the proof-of-concept.

If you need same-day handling for an exploited-in-the-wild issue, mark the
subject line `[SECURITY-URGENT]`.

## Supported versions

| Version | Status        | Security updates |
| ------- | ------------- | ---------------- |
| 1.x     | Current       | Yes              |
| < 1.0   | Unsupported   | No (upgrade)     |

The AF Stack runtime, dashboard, CLI, and SDK ship in lockstep — a security
fix lands as a single point release across the whole stack.

## Threat model

### In scope

| Threat                                  | Notes |
| --------------------------------------- | ----- |
| Operator session compromise             | Stolen better-auth cookie. Mitigation: short session TTL, CORS allowlist (Phase 16.1), cookie `HttpOnly`+`Secure`+`SameSite=Lax`. |
| Tenant API key theft                    | Bearer key leaked from a tenant deployment. Mitigation: bcrypt at rest, revoke endpoint, one-time-reveal. |
| Untrusted sandbox code                  | A workload module ships user-controlled code. Mitigation: adapter choice (docker = root-equiv, gVisor/Firecracker/e2b = stronger), CPU/memory/time caps. |
| Untrusted webhook payload (inbound)     | Provider sends a forged or replayed event. Mitigation: HMAC verify (`hmac.Equal`), dedup token, replay window. |
| Untrusted MCP server output             | Remote MCP server returns malicious tool output. Mitigation: schema validation, tool output is not executed. |
| Untrusted LLM provider output (prompt injection results) | LLM emits adversarial instructions. Mitigation: harness contract limits the action space; no system can guarantee non-injectable output. |
| Server-side request forgery (SSRF)      | User-supplied URL coerces the runtime into hitting private IPs. Mitigation: `safehttp` blocks private CIDRs + cloud metadata. |

### Explicitly out of scope

- **Physical host compromise.** If the box is owned, secrets are gone.
- **Postgres compromise.** RLS assumes the DB is a trusted dependency.
  An attacker with DB credentials bypasses tenant isolation.
- **Social engineering of operators.** We can't stop someone from emailing
  a tenant admin their own API key.
- **Side-channel attacks** (Spectre / Rowhammer / timing on the LLM
  provider's hardware).

## Trust boundaries

```
   ┌────────────────────────────────────────────────────────────────┐
   │ Operator browser                                               │
   └─────────────┬──────────────────────────────────────────────────┘
                 │ Same-origin proxy + better-auth session cookie
                 ▼
   ┌──────────────────────────┐    ┌─────────────────────────────┐
   │ Dashboard (Next.js)       │   │ SDK / external caller        │
   └─────────────┬─────────────┘    └──────────┬──────────────────┘
                 │                              │ Authorization: Bearer af_<...>
                 ▼                              ▼
   ┌────────────────────────────────────────────────────────────────┐
   │ Runtime (Go)                                                    │
   │  • tenant_resolver: bcrypt-verifies API keys, decodes cookies   │
   │  • RLS GUC: `set local app.tenant_id = <tid>` per txn           │
   │  • safehttp: refuses private CIDRs on every outbound dial       │
   └──────┬────────────┬────────────┬─────────────────┬──────────────┘
          ▼            ▼            ▼                 ▼
   ┌──────────┐ ┌───────────┐ ┌──────────┐  ┌─────────────────────┐
   │ Postgres │ │ Object    │ │ Sandbox  │  │ External services    │
   │ (RLS)    │ │ storage   │ │ adapter  │  │ (LLM, MCP, webhooks) │
   └──────────┘ └───────────┘ └──────────┘  └─────────────────────┘
```

| Boundary                          | Authn / authz                                                      | Trust state |
| --------------------------------- | ------------------------------------------------------------------ | ----------- |
| Operator → dashboard              | better-auth session cookie                                         | Trusted     |
| SDK / external → runtime          | Bearer `af_<prefix>_<secret>` (bcrypt) OR session cookie           | Trusted-on-verify |
| Runtime → Postgres                | RLS keyed on `app.tenant_id` GUC; runtime sets, DB enforces        | Trusted dep |
| Runtime → sandbox                 | Adapter-dependent (docker = root-equiv, gVisor / Firecracker / e2b = stronger) | Untrusted output |
| Runtime → external services       | LLM, S3, MCP, webhook destinations                                 | Untrusted output, untrusted destination URL |
| Inbound webhook → runtime         | HMAC verify + dedup token + replay window                          | Untrusted-on-arrival |

## What we guarantee

- **Cross-tenant data isolation** when the `multi-tenancy` module is enabled
  and the RLS GUC is set on every transaction. Tested in
  `internal/tenancy/*_test.go` and `migrations/00004_rls.sql`.
- **Secrets encrypted at rest** with AES-256-GCM, fresh random nonce per
  record, version byte bound into the GCM AAD so downgrade attacks fail.
  See `internal/secrets/crypto.go`.
- **Webhook payloads HMAC-verified** before any handler runs (constant-time
  `hmac.Equal`). See `internal/webhooks/verify.go` + `inbound.go`.
- **Sandbox CPU / time / memory caps** enforced by the adapter. Time cap
  surfaces as `status="timeout"`; memory cap kills the container.
- **Constant-time API-key comparison** via bcrypt. No string compare on
  secrets anywhere in the auth path.
- **No SSRF to private CIDRs** from the bundled HTTP clients — the
  `safehttp` package refuses 10/8, 172.16/12, 192.168/16, 127/8,
  169.254/16, 100.64/10, fc00::/7, fe80::/10, ::1, and the cloud
  metadata hostnames.
- **Audit log on secret reveal.** Every successful `POST /api/v1/secrets/{key}/reveal`
  inserts a `secret.reveal` row into `suite_audit_log`.

## What we don't guarantee

- **Prompt-injection-resistant LLM output.** No system can. Treat every
  LLM response as untrusted input and validate it against your domain
  schema before acting.
- **Arbitrary-code sandbox escapes.** The adapter is the boundary. Docker
  is root-equivalent on a shared kernel and should not be used for
  hostile workloads — pick gVisor, Firecracker, or e2b for that.
- **Egress filtering from sandboxes.** gVisor + Firecracker enforce a
  network namespace boundary; Docker does not. The runtime exposes
  `RunSpec.Network = isolated` for the Docker adapter, but `restricted`
  is currently identical to `open` (see `internal/sandbox/adapters/docker/adapter.go`).
- **DoS protection beyond the bundled rate limiter.** The runtime ships
  a token-bucket limiter on `/api/v1/agents/*`, `/api/v1/llm/*`, and
  `/api/v1/storage/upload`. For production deployments, put a WAF in
  front.
- **Confidentiality of bearer-token logs.** If you enable `LOG_LEVEL=debug`
  the runtime may log incoming Authorization-header prefixes for
  debugging. Don't ship debug logs to a third party.

## Cryptographic defaults

| Asset                    | Algorithm                                       |
| ------------------------ | ----------------------------------------------- |
| Secrets-at-rest          | AES-256-GCM, 12-byte nonce, version byte in AAD |
| API key secrets          | bcrypt cost 12                                  |
| Session tokens           | better-auth defaults (`crypto/rand`, 256 bits)  |
| Webhook HMAC             | sha256 (sha1 accepted for legacy providers)     |
| HMAC comparison          | `crypto/hmac` `Equal` (constant-time)           |
| Random nonces / tokens   | `crypto/rand`                                   |

## Hardening checklist (production)

- Set `AF_STACK_KMS_KEY` to a real 32-byte hex value. Without this, the
  runtime falls back to a **deterministic dev KEK** and logs a loud
  warning at boot. The dev KEK is not a secret.
- Set `AF_STACK_AUTH_SECRET` to a real 32-byte hex value.
- Set `AF_STACK_CORS_ORIGINS` to your dashboard's public origin. The
  default allowlist covers only `localhost:3000` + `127.0.0.1:3000`.
- Rotate `AF_STACK_S3_ACCESS_KEY` + `AF_STACK_S3_SECRET_KEY` away from
  the bundled `minio` / `minio-secret` defaults.
- Enable the `multi-tenancy` module. Without it, every request gets the
  default tenant and there is no per-request auth check — including
  `POST /api/v1/db/sql`.
- Run the sandbox under a non-root adapter (gVisor / Firecracker / e2b)
  for any workload that runs untrusted code.
