# Shipwright — autonomous coding-agent demo

> Customer pastes a repo + a task → the agent clones the repo, runs a coding
> harness (or an honest file-edit fallback when no harness binary is present),
> pushes a branch, and opens a **real pull request**. Iteration UI built on
> standard shadcn components.

This is the real flow: no simulated steps, no hardcoded diff. Given a
`GH_TOKEN` and a `repo_url`, the agent opens a genuine PR.

## What's here

```
examples/02-shipwright/
├── agents/shipwright/            # AgentField coding agent (node_id=shipwright, reasoner=build)
│   ├── Dockerfile                # python + git; installs httpx
│   ├── main.py                   # @app.reasoner def build(payload) — clone → harness → push → PR
│   ├── test_shipwright_agent.py  # unit tests (fake git + http runners; no network/token needed)
│   └── requirements.txt
├── handlers/                     # FastAPI workload-module sidecar
│   ├── handler.py                # /tasks POST/GET, /tasks/{id} GET, /tasks/{id}/cancel POST, /stats GET
│   └── requirements.txt
├── migrations/
│   └── 00001_shipwright.sql      # shipwright_tasks + shipwright_steps with RLS
└── docker-compose.yml            # Compose overlay
```

## The canonical agent call

One name, agreed across every layer: **`shipwright.build`**
(node_id `shipwright`, reasoner `build`).

- runtime Shipwright route (`services/runtime/.../shipwright.go`) defaults to it
- this compose sets `NODE_ID=shipwright` and `SHIPWRIGHT_AGENT=shipwright.build`
- the agent registers `node_id="shipwright"` with an `@app.reasoner` named `build`

## Run it

```bash
# From repo root, with .env populated (cp .env.example .env) — set GH_TOKEN.
docker compose \
  -f docker-compose.yml \
  -f examples/02-shipwright/docker-compose.yml \
  up -d

# Customer-app:    http://localhost:34000
# Runtime API:     http://localhost:8080
# Shipwright API:  http://localhost:38201
```

Sign up at `http://localhost:34000/sign-up` (first sign-up auto-provisions a
tenant + membership + API key), click **Shipwright**, and submit a task with a
repo URL you can open PRs against.

## How the flow works

```
Customer-app /shipwright form
        ↓  POST /api/customer/shipwright/tasks  (proxy attaches tenant + user)
shipwright-api sidecar (handler.py)
   ├── INSERT INTO shipwright_tasks (status=queued)
   └── async dispatch (non-blocking) →
        POST runtime /api/v1/agents/async/shipwright.build
        ↓  AgentField routes to the agent
shipwright-agent (main.py) — the REAL agent
   ├── shallow-clones repo_url with GH_TOKEN (token redacted in every log line)
   ├── cuts branch shipwright/<slug>-<taskid>
   ├── runs a coding harness (claude-code / codex / gemini / opencode) if one is
   │   installed + authed; otherwise applies an honest file-edit fallback
   ├── commits the real diff, pushes the branch
   └── opens a PR via the GitHub REST API → returns {pr_url, branch, diff}
        ↓
handler.py polls the execution and persists status + PR url
        ↓
Customer-app polls and renders the live state
```

## Harnesses

A harness is chosen only if its binary is installed **and** a matching
credential is set. Otherwise the agent falls back to a genuine (if minimal)
file edit so the PR still carries a real diff — it never fabricates one.

| provider    | binary   | credential (any one)                         |
|-------------|----------|----------------------------------------------|
| claude-code | `claude` | `ANTHROPIC_API_KEY` / `CLAUDE_CODE_OAUTH_TOKEN` |
| codex       | `codex`  | `OPENAI_API_KEY` / `OPENROUTER_API_KEY`      |
| gemini      | `gemini` | `GEMINI_API_KEY` / `GOOGLE_API_KEY`          |
| opencode    | `opencode` | (none required)                            |

Pin one with `SHIPWRIGHT_HARNESS=codex` and a model with `SHIPWRIGHT_MODEL`.
The base image ships no harness binary by default (keeps it small); the
file-edit fallback still opens a real PR. To use a real harness, extend the
Dockerfile to install it, or run the agent where the binary already exists.

## Live acceptance — assert a real PR

This is the H3 contract check. It needs a throwaway repo and a `GH_TOKEN`
with `repo`/PR scope, so it can't run in CI without that secret.

```bash
# 1. A throwaway repo you can open PRs against, e.g. github.com/<you>/shipwright-smoke
# 2. export GH_TOKEN=<pat-with-repo-scope>
# 3. Bring the stack up (above), sign up, grab your AF_STACK API key.

curl -sS -X POST http://localhost:8080/api/v1/shipwright/tasks \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"Add a hello file","description":"Create HELLO.md at the repo root.",
       "repo_url":"https://github.com/<you>/shipwright-smoke"}'

# Poll GET /api/v1/shipwright/tasks/<id> until status=completed, then open the
# PR url on GitHub and confirm it has a real diff. Zero sleeps, zero fake diff.
```

## Unit tests

```bash
cd examples/02-shipwright/agents/shipwright
python -m pytest test_shipwright_agent.py -q
```

They drive the whole clone → push → open-PR flow with fake subprocess + HTTP
runners, asserting: a real PR URL flows back, the git command sequence is
correct, the token never appears in a log line, and no PR is opened when the
harness produces no changes.

## Watch out for

- **Don't name a reasoner `run`** — it collides with `Agent.run()`. This agent
  uses `build`.
- **AgentField caches reasoner metadata** keyed on node_id; bump `AGENT_VERSION`
  or the node_id to force a fresh registration during iteration.
- **The token lives in `.git/config`** of the ephemeral clone (embedded in the
  authed remote URL). That's fine inside the throwaway container; never mount a
  persistent, shared workspace across tenants.

## Screenshots

See [`../../docs/assets/dashboard-screenshots/`](../../docs/assets/dashboard-screenshots/).
