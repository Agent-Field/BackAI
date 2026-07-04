# af-stack — operator CLI reference

The `af-stack` CLI is a thin, scriptable wrapper over the runtime REST API —
every admin command is a single HTTP call to the same surface the dashboard
uses, so if the CLI works, the REST API works. Every command in this reference
requires an **operator key** in `AF_STACK_API_KEY`; ordinary tenant keys are
rejected by the operator gate.

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
and exits `1`. The format is:

```
[CODE] message (status=N)
```

The runtime error envelope also carries a `request_id` (from
`error.request_id`) for correlating with server logs. Codes are stable strings
you can branch on in scripts — for example `BUDGET_EXCEEDED` when a key's budget
is exhausted. If the body is not a structured envelope, the CLI falls back to
`HTTP N: <raw body>`.

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

```bash
af-stack logs --level error --service runtime --search timeout --limit 100
```

REST endpoint: `GET /admin/logs?limit=&level=&service=&search=`.

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
