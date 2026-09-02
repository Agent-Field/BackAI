# Run It Locally

## Quick start

```bash
# from inside your clone of the BackAI repo
af-stack dev
```

From inside the clone, that's the whole thing — see the
[golden path](README.md) for the `git clone` line. `af-stack dev`:

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

`af-stack dev` also runs inside an app scaffolded by
`af-stack init <name>` — that app brings its own backend, so no clone is
involved. See [In a scaffolded app](#in-a-scaffolded-app) below. Anywhere
else (no checkout, no scaffold) it exits 1 and prints both ways in.

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

## In a scaffolded app

`af-stack init <name>` writes an app that carries its own backend: a
`docker-compose.yml` plus a `backend/` directory
(`backend/postgres-init.sh`, `backend/litellm-config.yaml`). The compose
file pulls the published BackAI release images, pinned to the version of
the CLI that scaffolded it — Postgres (pgvector), MinIO, LiteLLM, the
AgentField control plane, the runtime, the operator dashboard, and the
`supportdesk` demo agent with its no-key `echo` reasoner. Docker with
Compose is the only prerequisite (plus Node 18+ for the app itself).

Run `af-stack dev` from inside that app and it:

1. Allocates conflict-free host ports, writing `AF_STACK_PORT`,
   `AGENTFIELD_PORT`, `POSTGRES_PORT` and friends into the app's `.env`
   when the defaults (8080 / 8081 / 5432 / …) are busy.
2. Runs `docker compose up -d` and waits for the runtime's `/ready`.
3. Writes `AF_STACK_URL=http://localhost:<port>` into `.env` and prints the
   URLs — API runtime, operator dashboard
   (`http://localhost:33000`, `operator@af-stack.local` / `changeme123`),
   AgentField UI.

It is **detached**: it returns once the backend is ready. The first run
pulls the images, so give it a minute.

The scaffold's `package.json` wires that up for you:

| Command | Does |
| --- | --- |
| `npm start` | `prestart` runs `af-stack dev` (a no-op when the backend is already up), then the app lists the registered agents and calls `supportdesk.echo` |
| `npm run backend` | `af-stack dev` on its own |
| `npm run backend:stop` | `docker compose down` (add `-v` to drop the data volumes too) |

`af-stack init <name> --template saas` (a Vite/React starter) gets the same
bundled backend; there `npm run dev` boots it via a `predev` hook.

There is no customer app in the scaffolded backend — the app you scaffolded
is the customer app.

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
