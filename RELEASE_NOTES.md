# Release Notes

## v0.1.0-rc.1 — first release candidate

The first candidate cut of BackAI. It makes the **hero flow real end to end**
and lands the P0 (guardrails) and P1 (security) work. It is a *candidate*, not
a final release: the "known deferred" items below are tracked and gate the final
`v0.1.0`.

### The hero flow — now real end to end

`af-stack init --template coding-agent` scaffolds a multi-tenant coding-agent
app; sign-up mints a tenant + API key; a submitted task drives the **shipwright**
agent to clone the repo, run a coding harness (or an honest file-edit fallback
when no harness binary is present), push a branch, and open a **genuine pull
request** via the GitHub REST API; the run is metered as a per-tenant cost row.
No simulated steps, no hardcoded diff. See
[`examples/02-shipwright/`](examples/02-shipwright/).

Auth to GitHub is a tenant-supplied **`GH_TOKEN`** secret — the v1 path. The
GitHub-OAuth "connect a repo" UX is explicitly backlog.

### Security (P1)

- **S1 / S1b** — operator-gated the DB-studio and adapter-registry surfaces, and
  closed 5 unauthenticated cross-tenant read leaks (logs, secrets, runs, crons,
  skills).
- **S2** — runtime refuses to serve as an RLS-bypassing DB role; compose
  provisions a restricted serving role.
- **S3** — hardened Docker sandbox defaults (cap-drop, no-new-privileges, pid
  limits).
- **S4** — sandbox `network=restricted` is rejected rather than silently
  allowed.
- **S5** — a default per-tenant monthly spend ceiling is wired at boot.
- **S6** — KMS preflight: the runtime fails loudly when a configured KMS can't
  load instead of degrading silently.
- **S8** — object storage is tenant-scoped, closing a cross-tenant leak.

### Guardrails & CI (P0)

- CI runs on push/PR; **golangci-lint v2** restored as a blocking gate.
- **G2** — `scripts/acceptance.sh`, the hero-flow release tripwire.
- **G3** — truth-gate scaffolding in the skill verifier.

### DX & deploy

- **A1** — canonical error envelope + JSON 404 for agent errors.
- **A3** — adapter boot self-check (fails fast on an unknown adapter) + a wired
  `remote` sandbox adapter.
- **X1 / X2** — SKILL.md reframed CLI/API-first; the scaffold emits a root
  `CLAUDE.md` + `.mcp.json` for agent discovery.
- **D2** — deploy hardening: dropped the broken Fly `release_command`, pinned
  prod-compose images off `:latest`, aligned the dashboard build to Node 20.
- **R1** — docs truth pass: real adapter sets, the `GH_TOKEN`/connect-backlog
  story.

### Release-blockers caught by the R2 acceptance run

Running `scripts/acceptance.sh` against a fresh stack surfaced two first-boot
defects that CI structurally cannot catch (CI's Go tests run with no Postgres,
and the scaffold is only exercised via unit stubs):

- **First-boot migration crash.** `00027_block1_admin_endpoints.sql` had a
  `do $$ … end $$;` block without goose statement annotations, so the runner
  split it on an inner `;` → `unterminated dollar-quoted string` → the runtime
  crash-looped and never reached `/ready`. Fixed by wrapping the block in
  `-- +goose StatementBegin/StatementEnd`.
- **`af-stack init` aborted on a fresh clone.** `init` ran `pnpm run
  generate:brand` as a fatal step *before* scaffolding, so a clone without
  `node_modules` (the documented DX) aborted before the coding agent was
  written. Brand regeneration is now a warning — `brand.yaml` is the source of
  truth and CSS regenerates at build time — so the hero scaffold always lands.

### Truth gates — enforced vs. stubbed

The skill verifier (`skills/af-stack/meta/verify-skill.mjs`) carries three truth
gates. With `ENFORCE_TRUTH_GATES=1`:

- **`no-stub-agents`** — **green / enforce-ready.** The hero agent is real.
- **`openapi-parity`** — **stubbed.** 88 served-but-undocumented routes; enforcing
  is blocked until the OpenAPI spec catches up to the served surface.
- **`sdk-parity`** — **stubbed.** 6 namespace diffs between the TS and Python SDKs
  (tracked as A2); enforcing is blocked until parity lands.

Gates flip to enforcing per-gate as the underlying reality becomes true, so the
default is opt-in until then.

### Known deferred (not in rc.1 — gate the final v0.1.0)

- **A2** — SDK parity across the 6 diverging namespaces (`sdk-parity` gate stays
  stubbed until closed).
- **openapi-parity** — 88 served routes to document (`openapi-parity` gate stays
  stubbed until the spec catches up).
- **D1** — the customer app on all deploy targets (Helm / Fly / Render /
  prod-compose / Caddy); needs a domain-topology decision.
- **S1b (cost + billing)** — 2 remaining principal-scoped read surfaces; needs an
  auth-model decision (do operators carry a tenant membership?).
- **H4** — publish `@af-stack/sdk` to npm; needs npm credentials.
- **GitHub connect OAuth UX** — backlog; `GH_TOKEN` secret is the v1 path.

### Running the acceptance tripwire

```bash
./scripts/acceptance.sh
```

Phases 1–5 (boot → runtime ready → multi-tenancy on → scaffold → agent
registered) enforce today. Phases 6–8 (tenant + API key → real PR → metered cost
row) require `OPERATOR_TOKEN`, `GH_TOKEN`, and `ACCEPT_REPO` (a throwaway repo);
they SKIP until those are supplied. The tripwire is fully green only when the
entire hero flow is real with credentials present.
