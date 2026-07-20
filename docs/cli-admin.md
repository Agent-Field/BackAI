# af-stack — operator CLI reference

The `af-stack` CLI is a thin, scriptable wrapper over the runtime REST API —
every command is a single HTTP call to the same surface the dashboard uses, so
if the CLI works, the REST API works.

Most commands here are **operator** commands: they need an operator key in
`AF_STACK_API_KEY` and ordinary tenant keys are rejected by the operator gate.
The app-developer commands added at the end — [`connection`](#connections) and
[`secrets`](#secrets) — instead ride the **tenant** key that owns the resource,
and [`job new`](#jobs-scaffold) needs no key at all (it is a local scaffold).
Each command's section states which key it wants.

## Setup

Two environment variables control every admin call:

| Env var            | Purpose                                                     | Default                  |
| ------------------ | ----------------------------------------------------------- | ------------------------ |
| `AF_STACK_URL`     | Runtime base URL. The CLI appends `/api/v1` to it.          | `http://localhost:8080`  |
| `AF_STACK_API_KEY` | Bearer token — must be an **operator** key for admin cmds.  | (unset)                  |

Requests go to `${AF_STACK_URL}/api/v1<endpoint>` with
`Authorization: Bearer ${AF_STACK_API_KEY}`.

### Minting an operator key

Operator keys are minted directly against the database, so the bootstrap
commands need `DATABASE_URL` set (they do **not** go through the REST API):

```bash
# 1. Allow an operator (records the email as operator-eligible)
af-stack operator create --email founder@example.com

# 2. Mint an operator API key (needs DATABASE_URL for direct DB access)
export DATABASE_URL=postgres://...
af-stack operator key --owner        # --owner grants the operator:owner scope

# 3. Use the printed key for every admin command
export AF_STACK_API_KEY=<printed-key>
export AF_STACK_URL=http://localhost:8080
```

The key is scoped to the zero-uuid operator tenant with scope `operator` (or
`operator:owner` with `--owner`).

## Error handling

On any non-2xx response the CLI prints the runtime's structured error envelope
to stderr. The format is:

```
[CODE] message (status=N)
```

The runtime error envelope also carries a `request_id` (from
`error.request_id`) for correlating with server logs. Codes are stable strings
you can branch on in scripts — for example `BUDGET_EXCEEDED` when a key's budget
is exhausted. If the body is not a structured envelope, the CLI falls back to
`HTTP N: <raw body>`.

### Exit codes

The process exit code is a stable machine contract, so an agent can branch on
**why** a command failed without scraping stderr:

| Exit | Meaning                                                            |
| ---- | ----------------------------------------------------------------- |
| `0`  | success                                                           |
| `1`  | unclassified failure                                              |
| `2`  | bad flags/args or unknown command — nothing ran                   |
| `3`  | missing/invalid credentials (runtime `401`/`403`)                 |
| `4`  | target does not exist (runtime `404`)                             |
| `5`  | input failed validation (runtime `400`/`409`/`422`)               |
| `6`  | runtime/API error or unreachable (`5xx`, transport, missing route)|

Commands that support machine output take `--json` and emit a stable JSON
document on stdout; the same call site renders the human table otherwise, so
the two representations never drift.

The machine-readable source of truth for the full API — every endpoint, field,
and error code — is the OpenAPI document at `GET /api/v1/openapi.json`.

## Keys

Manage tenant API keys: list, issue, rotate, revoke, and inspect spend.

| Subcommand            | Flags / args                                             | REST endpoint                       |
| --------------------- | -------------------------------------------------------- | ----------------------------------- |
| `keys list`           | `--tenant <id>` (filter)                                 | `GET /admin/keys[?tenant=<id>]`     |
| `keys issue`          | `--tenant <id>` (required), `--name`, `--scopes`, `--budget` | `POST /admin/keys`             |
| `keys rotate <id>`    | key id (positional)                                      | `POST /admin/keys/<id>/rotate`      |
| `keys revoke <id>`    | key id (positional)                                      | `DELETE /admin/keys/<id>`           |
| `keys spend <id>`     | key id (positional)                                      | `GET /admin/keys/<id>/spend`        |

`--scopes` is a comma-separated list; `--budget` sets `budget_max_usd` (0 =
none). The full key value is printed exactly once on `issue`/`rotate`.

```bash
af-stack keys issue --tenant 3f2c... --name ci --scopes read,write --budget 25
af-stack keys list --tenant 3f2c...
af-stack keys rotate key_abc123
af-stack keys revoke key_abc123
af-stack keys spend  key_abc123
```

## Agents

List the agents currently registered with the runtime, with their versions,
reasoners, and tags.

```bash
af-stack agents list
```

REST endpoint: `GET /agents` (only the `list` subcommand exists).

## Reasoners

Per-reasoner analytics: call counts, error rate, average latency, and cost over
a time window.

| Flag       | Meaning                                | Default      |
| ---------- | -------------------------------------- | ------------ |
| `--from`   | Window start (RFC3339)                 | 24h ago      |
| `--to`     | Window end (RFC3339)                   | now          |
| `--limit`  | Max rows                               | 100          |

```bash
af-stack reasoners --limit 20 --from 2026-07-01T00:00:00Z
```

REST endpoint: `GET /reasoners/analytics?limit=&from=&to=`.

## Runs

Recent agent runs, newest first, with tenant, duration, and cost.

| Flag       | Meaning                              | Default |
| ---------- | ------------------------------------ | ------- |
| `--agent`  | Filter by agent/reasoner label       | (all)   |
| `--status` | Filter: `succeeded` \| `failed`      | (all)   |
| `--limit`  | Max runs                             | 25      |

```bash
af-stack runs --agent researcher --status failed --limit 50
```

REST endpoint: `GET /runs?agent=&status=&limit=`.

## Logs

Tail runtime log lines with level, service, and substring filters.

| Flag        | Meaning                                     | Default |
| ----------- | ------------------------------------------- | ------- |
| `--level`   | Min level: `debug` \| `info` \| `warn` \| `error` | (all) |
| `--service` | Service filter                              | (all)   |
| `--search`  | Substring search                            | (none)  |
| `--limit`   | Max lines                                   | 50      |
| `--tail`    | Show only the most recent N lines (alias for `--limit`) | (unset) |
| `--since`   | Only lines at/after a time (RFC3339 or a duration like `15m`) | (none) |
| `--json`    | Emit the lines as a `{ "logs": [...] }` document | off |

```bash
af-stack logs --level error --service runtime --search timeout --limit 100
af-stack logs --tail 100 --since 15m --json
```

REST endpoint: `GET /admin/logs?limit=&level=&service=&search=&since=`. A
runtime that does not expose this route degrades to exit code `6` (remote /
capability gap) rather than a bare `404`, so scripts can tell "no logs surface
here" apart from "that target is missing".

## Errors

Manage error groups: list them and change their status (resolve / mute /
reopen).

| Subcommand            | Flags / args                            | REST endpoint                        |
| --------------------- | --------------------------------------- | ------------------------------------ |
| `errors list`         | `--status open\|muted\|resolved`, `--limit` (50) | `GET /admin/errors?status=&limit=` |
| `errors resolve <id>` | group id (positional)                   | `POST /admin/errors/<id>/resolve`    |
| `errors mute <id>`    | group id (positional)                   | `POST /admin/errors/<id>/mute`       |
| `errors reopen <id>`  | group id (positional)                   | `POST /admin/errors/<id>/reopen`     |

`list` is the default subcommand when none is given.

```bash
af-stack errors list --status open --limit 25
af-stack errors resolve grp_9a1b
af-stack errors mute    grp_9a1b
af-stack errors reopen  grp_9a1b
```

## Audit

The operator audit log — a record of privileged actions, filterable by tenant
and action.

| Flag       | Meaning                | Default |
| ---------- | ---------------------- | ------- |
| `--tenant` | Filter by tenant id    | (all)   |
| `--action` | Filter by action       | (all)   |
| `--limit`  | Max entries            | 50      |

```bash
af-stack audit --tenant 3f2c... --action key.revoke --limit 100
```

REST endpoint: `GET /admin/audit?tenant=&action=&limit=`.

## Sessions

Inspect and revoke live user sessions (both customer and operator sessions).

| Subcommand              | Flags / args                              | REST endpoint                     |
| ----------------------- | ----------------------------------------- | --------------------------------- |
| `sessions list`         | `--email <substr>`, `--limit` (50)        | `GET /admin/sessions?email=&limit=` |
| `sessions revoke <id>`  | session id (positional)                   | `DELETE /admin/sessions/<id>`     |

`list` is the default subcommand when none is given.

```bash
af-stack sessions list --email founder@
af-stack sessions revoke sess_7c2d
```

## Tenants

List all tenants on the stack.

```bash
af-stack tenants list
```

REST endpoint: `GET /admin/tenants` (only the `list` subcommand exists).

## Activity

The cross-tenant customer activity log, filterable by tenant and action.

| Flag       | Meaning                | Default |
| ---------- | ---------------------- | ------- |
| `--tenant` | Filter by tenant id    | (all)   |
| `--action` | Filter by action       | (all)   |
| `--limit`  | Max entries            | 50      |

```bash
af-stack activity --tenant 3f2c... --action agent.run --limit 100
```

REST endpoint: `GET /admin/activity?tenant=&action=&limit=`.

---

The remaining commands are **app-developer** commands, not operator commands.

## Connections

Manage external-service connections (R5): a connection stores a credential
**server-side**, and app code later calls the provider through the handle
(`POST /connections/{id}/request`) so the runtime injects the secret. Uses the
**tenant** key that owns the connection.

| Subcommand           | Flags / args                                                        | REST endpoint                  |
| -------------------- | ------------------------------------------------------------------- | ------------------------------ |
| `connection add`     | `--provider` (req), `--kind api_key\|oauth` (req), `--name`, `--scopes`, `--credential-stdin` | `POST /connections`  |
| `connection list`    | —                                                                   | `GET /connections`             |
| `connection remove <id>` | `--yes` (skip confirm)                                           | `DELETE /connections/{id}`     |

A credential is **never** passed as a CLI argument (it would leak into shell
history). For `--kind api_key`, the CLI prompts for the secret, or reads the
whole of stdin when `--credential-stdin` is set. For `--kind oauth`, `add`
prints the authorize URL the runtime returns — open it in a browser to
complete the consent round-trip. `list` shows metadata + health only, never a
credential. All three support `--json`.

```bash
# API-key connection — secret comes from stdin, never argv:
echo -n "$GITHUB_TOKEN" | af-stack connection add \
  --provider github --kind api_key --name ci --credential-stdin

# OAuth connection — prints the URL to authorize:
af-stack connection add --provider google --kind oauth --name gcal

af-stack connection list --json
af-stack connection remove 3f2c... --yes
```

## Secrets

Read and write the caller tenant's secrets vault
(`/api/v1/vault/secrets`). Uses the **tenant** key. A secret **value** never
travels through argv: `set` reads it from a prompt, or from the whole of stdin
with `--value-stdin`. `list` shows metadata + the `secret:<key>` reference
only — never the plaintext, which the vault reveals solely through its audited
`/reveal` path.

| Subcommand        | Flags / args                              | REST endpoint                    |
| ----------------- | ----------------------------------------- | -------------------------------- |
| `secrets set <key>` | `--value-stdin`, `--description`        | `PUT /vault/secrets/{key}`       |
| `secrets list`    | —                                         | `GET /vault/secrets`             |

```bash
echo -n "$STRIPE_KEY" | af-stack secrets set stripe --value-stdin
af-stack secrets set openai            # prompts for the value
af-stack secrets list --json
```

Drop the printed `secret:<key>` reference into app config (e.g. an MCP server's
env value) instead of the plaintext.

## Jobs (scaffold)

Scaffold a pull-based background-worker job (R3). This is a **local** command —
no runtime, no key. It writes `jobs/<name>.py` (or `.ts`) wired to the worker
SDK: it registers the job kind, a handler that receives `(payload, ctx)`, and a
run entrypoint that leases and executes work.

| Subcommand        | Flags / args                     | Output                          |
| ----------------- | -------------------------------- | ------------------------------- |
| `job new <name>`  | `--lang py\|ts` (default `py`)   | `jobs/<name>.<ext>` + next steps |

```bash
af-stack job new resize-image --lang py     # -> jobs/resize-image.py
af-stack job new thumbnailer --lang ts --json
```

Run the generated worker with a tenant API key that carries the `jobs:work`
scope (`af-stack keys issue --scopes jobs:work ...`); the printed next-steps
spell this out. `--json` emits `{ "created": ["jobs/<name>.<ext>"] }`.

## Diagnostics & migrations

These app-developer commands round out the CLI; each has its own `--json`
schema where it reports state.

| Command            | What it does                                                  | Key |
| ------------------ | ------------------------------------------------------------ | --- |
| `status [--json]`  | Compact "is the stack up and how is it configured" snapshot  | optional |
| `doctor [--json]`  | Environment + runtime health checks                          | optional |
| `test [--json]`    | Shippable-fork gates (module manifests, migration RLS lint)  | none |
| `db diff\|push\|generate\|reset` | Runtime + workload-module migrations via goose | `DATABASE_URL` |

```bash
af-stack status --json
af-stack db diff              # applied vs pending migrations
af-stack db reset --yes       # DESTRUCTIVE: roll every migration back
```
