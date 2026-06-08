# 02 — Shipwright

Shipwright is the autonomous coding-agent factory example for AF Stack.
Customers submit a task through AF Stack:

```bash
POST /api/v1/shipwright/tasks
```

AF Stack stores the task row, starts the AgentField reasoner
`shipwright.build`, and keeps only the returned `run_id` plus final patch
pointers. AgentField owns the live execution graph, harness calls, logs,
spans, traces, and memory.

## Quickstart

```bash
cd examples/02-shipwright
cp .env.example .env
# edit .env — set OPENROUTER_API_KEY or another provider key

docker compose -f docker-compose.yml up --build

# in another shell:
./scripts/run-task.sh
```

By default the agent uses `openrouter/google/gemini-2.5-flash` when
`OPENROUTER_API_KEY` is present. You can override per task with the API
`model` field.

Set `GH_TOKEN` to allow Shipwright to push a branch and open a draft
GitHub PR for GitHub repositories. Without `GH_TOKEN`, the harness path
still captures a durable patch file under the `shipwright-patches`
Docker volume and returns that file URI as `diff_url`.

## Architecture

```text
POST /shipwright/tasks
  -> AF Stack inserts suite_shipwright_tasks
  -> AgentField async execute: shipwright.build

shipwright.build
  -> triage_task        (.ai: complexity, risk, confidence)
  -> plan_change        (.ai: files, steps, tests, confidence)
  -> execute_plan       (clone repo -> app.harness(cwd=repo) -> git diff)
  -> review_patch       (.ai: approve/reject, findings, confidence)
  -> callback_complete  (POST /shipwright/tasks/{id}/complete)
```

The harness path is guarded with `shutil.which()`. The example image
installs the Codex, Gemini, and Claude Code CLIs so the default Compose
path can exercise `app.harness(provider=..., cwd=...)` instead of only
documenting it. Provider auth still comes from your environment:

- `codex` uses `OPENAI_API_KEY` or read-only `~/.codex/auth.json` plus
  `~/.codex/config.toml` mounts over a writable container-side Codex
  state volume.
- `gemini` uses `GOOGLE_API_KEY` or the read-only `~/.gemini` mount.
- `claude-code` uses `ANTHROPIC_API_KEY` or the read-only `~/.claude`
  mount.

The Compose file mounts local CLI auth into the agent container for
development. In production, replace those mounts with secrets or
provider API keys.

If the selected CLI is unavailable or unauthenticated, Shipwright falls
back to an `.ai()` patch sketch and marks that mode explicitly.

The default Compose setting is
`SHIPWRIGHT_HARNESS_PERMISSION_MODE=danger-full-access` because the
agent is already isolated in its own Docker container and Codex's nested
Linux sandbox can fail on Docker Desktop / kernels without unprivileged
user namespaces. For bare-metal Linux agents, switch this to the
stricter mode your harness supports.

When a harness is available, Shipwright:

1. Clones `repo_url` into a temporary working directory.
2. Checks out a `shipwright/...` branch.
3. Runs the harness in that checkout.
4. Captures `git diff --binary` into the `shipwright-patches` volume.
5. If `GH_TOKEN` is configured and `repo_url` is a GitHub repo, commits,
   pushes the branch, and opens a draft PR. The PR URL becomes `diff_url`.

When no harness binary is available, the agent falls back to an `.ai()`
patch sketch and explicitly does not claim files were edited.

## Files

```text
02-shipwright/
├── README.md
├── .env.example
├── docker-compose.yml
├── Dockerfile
├── agents/shipwright/main.py
├── agents/shipwright/requirements.txt
└── scripts/run-task.sh
```
