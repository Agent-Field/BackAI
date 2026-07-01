# Example Walk: Shipwright

> **Status — design vision, not the shipped example.** This document is the
> aspirational end-state used to pressure-test the platform's module set. The
> **shipped** demo is simpler and lives at
> [`examples/02-shipwright/`](../examples/02-shipwright/) — read its
> [`README`](../examples/02-shipwright/README.md) for what actually runs today.
>
> What's real in the shipped example: a **single** `shipwright.build` agent that
> clones a repo, runs a coding harness (or an honest file-edit fallback), pushes
> a branch, and opens a **real** PR via the GitHub REST API. Auth to GitHub is a
> **`GH_TOKEN` secret** the tenant supplies — the "connect a repo" GitHub-OAuth
> UX described below is explicitly **backlog** (see the release plan's Backlog
> section), not v1. Also aspirational here and **not** in the shipped example:
> Firecracker/Flintlock sandboxing (dev uses the `docker` adapter; `firecracker`
> and `e2b` are options), Lago/Stripe metering, the `swe-af` multi-agent
> fan-out, and `af-suite import-module`. Treat the flow below as the roadmap.

Validation workload #2: the **heavy AI** case. Sandbox-critical, multi-tenant
with untrusted code, long-running agent fan-outs.

## Product

Users sign up with GitHub, point at a repo, give a goal, get a PR opened.

## Modules enabled

| Module | State | Adapter |
|---|---|---|
| identity | on | better-auth + GitHub OAuth |
| multi-tenancy | on | PG RLS |
| public-gateway | on | suite native |
| llm-gateway | on | LiteLLM via AF |
| jobs | on | River |
| crons | on | River cron |
| storage | on | MinIO (dev) → S3 (prod) |
| secrets-vault | on | PG + KMS, rotation enabled |
| notifications | on | log-stub → Postmark |
| webhooks-in | on | Svix (GitHub events) |
| billing | on | Stripe + Lago metering |
| dashboard | on, customized | shadcn |
| **sandbox** | **on, critical** | Firecracker via Flintlock (prod) OR e2b (early days) |
| **git-workload** | **on** | suite module |
| search | minimal | PG FTS for build history |

Disabled: multimodal-storage, change-stream-listener.

15/16 modules on. Sandbox and git are the show.

## Repo structure

```
shipwright/
  docker-compose.yml
  docker-compose.prod.yml
  deploy/helm/
  
  apps/backend/
    agents/
      swe-af/                       # imported via:
        ...                          # af-suite import-module github.com/agent-field/swe-af
      shipwright-extras/
        cost_estimator.py
        repo_classifier.py
    handlers/
      builds.py
      repos.py
      settings.py
    jobs/
      cleanup_workspaces.py
      sync_github_repos.py
    crons/
      daily_workspace_gc.py
    migrations/
      001_builds.sql
      002_repos.sql
      003_github_tokens.sql
    config.yaml
```

## End-to-end flow: user submits a build

1. User signs up via GitHub OAuth → **identity** creates user → MT creates
   tenant → **secrets-vault** stores GH token, scoped to tenant
2. User connects a repo, submits goal → `POST /api/v1/builds`
3. **public-gateway** authn, rate-limits, forwards to handler
4. Handler calls `shipwright-extras.estimate_build_cost` (custom AF agent) →
   `.ai()` → cost estimate
5. **billing** module checks budget → OK
6. Handler persists build, calls `swe-planner.build` async
7. **AF** queues the call in its durable queue
8. Coding-agent planner reads goal, spawns architect -> N coders -> QA -> reviewer
9. Each coder agent calls `app.harness("...", provider="claude-code")`
10. Claude Code, when it runs `Bash`, calls `app.sandbox.run(...)` →
    **sandbox** module routes to Firecracker pool
11. microVM spawns with worktree mounted, network restricted to npm registry
12. `npm install`, tests, edits all inside the microVM
13. Code diff produced → AF records trace + tokens
14. Reviewer confirms → planner uses **git-workload** module to open PR
15. **AF** emits webhook to `/internal/builds/{id}/done`
16. **notifications** sends "PR opened" via Postmark
17. **billing.metering** rolls up LLM tokens + sandbox_seconds → Lago events
    → Stripe invoices
18. GitHub PR comment arrives → **Svix** verifies HMAC → triggers
    `swe-planner.address_feedback` → loop

Single build touches: identity → MT → gateway → AF (heavy) → llm-gateway →
sandbox (heavy) → git-workload → notifications → billing/metering →
webhooks-in. **Ten modules in concert.**

Critically: the coding-agent code did not change. `app.sandbox.run()`
absorbed the difference between dev (Docker) and prod (Firecracker).

## Code samples

**Custom estimator (one of two custom AF agents):**
```python
@app.reasoner()
async def estimate_build_cost(goal: dict) -> dict:
    estimate = await app.ai(
        system="Estimate USD cost for a coding-agent run.",
        user=f"Goal: {goal['text']}\nRepo size: {goal['loc']} LOC",
        schema=CostEstimate,
    )
    return estimate.model_dump()
```

**Build kickoff handler:**
```python
@handler.post("/api/v1/builds")
async def create_build(body: dict):
    estimate = await app.call("shipwright-extras.estimate_build_cost", body)
    if not await suite.billing.has_budget(amount_usd=estimate["estimated_usd"]):
        raise PaymentRequired("Monthly budget exceeded")
    
    build = await db.builds.insert({
        "tenant_id": ctx.tenant_id,
        "goal": body["goal"],
        "repo_url": body["repo_url"],
    })
    
    await app.call_async(
        "swe-planner.build",
        input={
            "goal": body["goal"],
            "repo_url": body["repo_url"],
            "secrets": {"gh_token": await suite.secrets.get("github_token")},
        },
        webhook_url=f"https://shipwright.dev/internal/builds/{build.id}/done",
    )
    return {"build_id": build.id, "estimate_usd": estimate["estimated_usd"]}
```

**Sandbox call (unchanged from the coding-agent code):**
```python
result = await app.sandbox.run(
    image="node:20-alpine",
    command=["npm", "test"],
    files={"/work": worktree_path},
    timeout_s=600,
    network="restricted",
    allow_egress=["registry.npmjs.org"],
    workspace_id=f"tenant-{tenant_id}-build-{build_id}",
)
```

## What dev did NOT have to build

- GitHub OAuth flow
- Secret storage with rotation
- Sandbox infrastructure (the actual Firecracker pool)
- LLM cost tracking + per-tenant attribution
- Stripe + Lago integration
- Webhook handling for GitHub (HMAC, replay, dedup via Svix)
- Per-tenant isolation (RLS handled it)
- AF DAG visualization (suite dashboard embeds AF)
- Auto-scaling sandbox workers (sandbox-host pool)

## What dev DID build

- Custom UI for "submit a build"
- Two small custom AF agents (cost estimator, classifier)
- The `POST /builds` handler
- The cleanup cron job
- Branding on the end-user dashboard scaffold

Rough split: **~80% suite primitives, ~20% product-specific**. The viral
promise made concrete.

## Pain points

1. Firecracker needs KVM, doesn't run on Mac. Dev uses Docker adapter
   locally, Firecracker in prod. Document the swap.
2. GitHub webhook dedup when GH retries — Svix handles HMAC; dev still needs
   idempotency in handler. Document the pattern.
3. BYO LLM keys UI is custom. Document "BYO key" pattern.
4. Build progress streaming — AF has SSE; suite needs to expose through
   gateway with tenant scoping. Document.
5. Refund on failed build — billing aggregates usage; failed builds might
   want auto-refund via the `billing.pre_charge` hook.

None are blockers. All are documentation, not redesign.

## What this validation proves

- Sandbox interface holds across dev (Docker) → prod (Firecracker)
- Multi-tenancy module + PG RLS isolates everything correctly
- Heavy AF usage (~20 agents) sits alongside custom agents without conflict
- Suite primitives compose: 10 modules touched per build, the dev's mental
  load was 3-4
- The "import the domain app as a module" mechanism is the key to reuse
