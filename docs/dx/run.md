# Run It Locally

## Quick start

```bash
# from inside your clone of the BackAI repo
af-stack dev
```

From inside the clone, that's the whole thing — see the
[golden path](README.md) for the `git clone` line. Run it anywhere else and
it exits 1 with `must run from inside a BackAI checkout — a clone of
https://github.com/Agent-Field/backai …`, followed by the clone command and
the standalone-app alternative. `af-stack dev`:

1. Runs a **port preflight** (`scripts/preflight.mjs --fix`) — finds a
   free host port for each service, writes the overrides into `.env`, and
   sets `COMPOSE_PROJECT_NAME`. Skip it with `--no-preflight`.
2. Runs `docker compose up` and prints the local URL map. Add `--detach`
   to background it — in detached mode it also opens the **customer app**
   (`http://localhost:34000` by default) in your browser; `--no-open`
   suppresses that. In the foreground nothing is opened, so `--no-open` on
   its own does nothing.

Prefer raw compose? `docker compose up` works too — but then you own port
conflicts yourself.

## Local URLs

After `af-stack dev`, the default endpoint map:

| Service | URL |
| --- | --- |
| Customer app | http://localhost:34000 |
| Admin dashboard | http://localhost:33000 |
| Runtime API | http://localhost:8080/api/v1 (health: `/health`) |
| AgentField control plane | http://localhost:8081 |
| MinIO console | http://localhost:9001 (S3 API on `:9000`) |
| LiteLLM | http://localhost:4000 |
| Postgres | `localhost:5432` |

These are the **defaults**. If a port is busy, preflight bumps it to the
next free one and records the override in `.env` (e.g.
`AF_STACK_DASHBOARD_PORT`, `AF_STACK_CUSTOMER_APP_PORT`, `AF_STACK_PORT`,
`AGENTFIELD_PORT`, `MINIO_CONSOLE_PORT`) — so read `.env` / the printed map
if a URL above doesn't respond.

## `.env`

`af-stack dev` reads and writes `.env`. Start from `.env.example`. Preflight
manages the port variables; you set the meaningful ones (provider keys,
mode, operator overrides). Full reference: [../CONFIGURATION.md](../CONFIGURATION.md).

## Default seeded operator

On first boot the dashboard seeds one operator so you can log in
immediately:

| | Default | Override with |
| --- | --- | --- |
| Email | `operator@af-stack.local` | `AF_STACK_DEFAULT_OPERATOR_EMAIL` |
| Password | `changeme123` | `AF_STACK_DEFAULT_OPERATOR_PASSWORD` |

Change the password after first login. (In **personal** mode there's no
login at all — see below.)

## Personal vs SaaS mode

One toggle, one env var (`AF_STACK_MODE`):

```bash
af-stack mode              # print current mode
af-stack mode personal     # single-user: no login, no billing
af-stack mode saas         # multi-tenant: auth + billing per module flags
```

- **`personal`** — no login, no billing surfaces; everything runs under
  the default tenant. Great for solo/self-hosted use.
- **`saas`** (default) — multi-tenant; auth and billing are governed by
  their module flags.

`af-stack mode` just writes `.env`; re-run `af-stack dev` to apply it.
