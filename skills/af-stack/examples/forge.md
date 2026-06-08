# Example — Forge (GitHub PR review co-pilot)

A reactive single-shot agent. Customer installs Forge as a GitHub App;
every PR fires a webhook; an agent in a sandbox reviews and posts
inline comments. Sold per-developer-seat.

**Comparable products**: CodeRabbit, Greptile, Sweep.

**Build time on AF Stack**: ~1 week of one developer.

## The 4 edit surfaces (the full picture)

| Surface | Path | What you write |
|---|---|---|
| Customer App | `apps/customer-app/src/app/(app)/` | 3–5 pages: dashboard, repos, billing, settings |
| Agent | `apps/backend/agents/forge/` | 1 reasoner: `review_pr` |
| Workload Module | `examples/forge/handlers/` | 3 routes (webhook, list reviews, stats) + 1 job + 1 migration |
| Dashboard Plugin | `apps/dashboard/plugins/forge/` | 1 page: cross-tenant stats |

## What's pre-wired (you don't write)

- HMAC verification on the GitHub webhook (`internal/webhooks/inbound.go`)
- Multi-tenancy isolation (PG RLS + `tenancy` middleware)
- Per-customer cost tracking (LLM gateway hooks → `suite_cost_events`)
- Sign-up + sign-in + GitHub OAuth (better-auth)
- Sandbox lifecycle (`internal/sandbox/`)
- Agent registration + invocation (AgentField)
- Secrets vault for the GitHub App private key (`internal/secrets/`)
- Job queue with retries (River)
- Customer-facing billing (Stripe or Lago)
- Operator dashboard shell + customer-app shell
- Deploy targets (Helm / Fly / Railway / Render / compose)
- OpenAPI auto-includes Forge routes

## Request flow

```
GitHub                  AF Stack runtime           Sandbox + Agent           GitHub API
  │
  │ PR opened
  ├─► POST /workload/forge/webhooks/github ──┐
  │   (HMAC signed)                          │
  │                              ① verify HMAC (webhooks-in)
  │                              ② lookup forge_repo by installation_id
  │                              ③ bind tenant context (PG RLS)
  │                              ④ enqueue River job
  │ ◄── 202 Accepted ────────────┘
  │                              ⑤ River worker picks up
  │                                 suite.agents.call("forge.review_pr", ...)
  │                                                          │
  │                                                          ▼
  │                                                    AgentField agent
  │                                                    apps/backend/agents/forge/
  │                                                          │
  │                                                          ▼
  │                                                    app.harness("claude-code")
  │                                                          │
  │                                                          ▼
  │                                                    suite.sandbox.run({...})
  │                                                          │
  │                                                          ▼
  │                                                    returns [Comment{...}, ...]
  │                                                          │
  │                              ⑥ Job continues:
  │                                 - fetch GitHub token from secrets
  │                                 - POST comments ─────────┼──────────────────────────┤
  │                                 - insert forge_reviews row, record cost
  │
  │ ⑦ Customer sees review on PR ◄──────────────────────────────────────────────────────
```

## Surface 1 — The agent

`apps/backend/agents/forge/Dockerfile`:

```dockerfile
FROM agentfield/python-base:latest

# Harness: claude-code (the OSS-AUDIT pattern — harnesses live in agent
# container, not runtime container)
RUN npm install -g @anthropic-ai/claude-code

# Sandbox tools the agent needs
RUN apt-get update && apt-get install -y git

COPY pyproject.toml /app/
COPY main.py /app/
WORKDIR /app
RUN pip install -e .

CMD ["python", "main.py"]
```

`apps/backend/agents/forge/main.py`:

```python
from agentfield import Agent
from pydantic import BaseModel
from typing import Literal

app = Agent(node_id="forge")

class ReviewComment(BaseModel):
    file: str
    line: int
    severity: Literal["info", "warning", "critical"]
    body: str

class ReviewResult(BaseModel):
    comments: list[ReviewComment]
    summary: str
    test_pass: bool | None

@app.reasoner(tags=["code-review"])
async def review_pr(payload: dict) -> dict:
    """Review a PR using Claude Code in a sandbox."""
    review = await app.harness(provider="claude-code").run(
        prompt=f"""You are reviewing a pull request.
Focus area: {payload.get('focus', 'all')}.
Generate inline review comments and report whether tests pass.""",
        sandbox=dict(
            image="node:20-alpine",
            setup=[
                f"git clone {payload['repo_url']} /work && cd /work",
                f"git checkout {payload['pr_branch']}",
                f"git diff {payload['base_branch']}..{payload['pr_branch']} > /tmp/diff.patch",
            ],
            test_command="npm test --silent || true",
            timeout_s=120,
        ),
        schema=ReviewResult,
    )
    return review.model_dump()

# __capabilities__ — copy from snippets/agent.py
# ...

if __name__ == "__main__":
    app.run()
```

**~50 lines for the entire agent.**

## Surface 2 — The workload module (Python sidecar)

`examples/forge/migrations/00001_forge.sql`:

```sql
CREATE TABLE forge_repos (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       text NOT NULL,
    github_repo     text NOT NULL,
    installation_id bigint NOT NULL,
    focus           text DEFAULT 'all',
    enabled         boolean DEFAULT TRUE,
    created_at      timestamptz DEFAULT now(),
    UNIQUE (tenant_id, github_repo)
);
ALTER TABLE forge_repos ENABLE ROW LEVEL SECURITY;
CREATE POLICY forge_repos_tenant_isolation ON forge_repos
    USING (
        current_setting('app.bypass_rls', true) = 'on'
        OR tenant_id = current_setting('app.tenant_id', true)
    );

CREATE TABLE forge_reviews (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id     uuid NOT NULL REFERENCES forge_repos(id),
    pr_number   int NOT NULL,
    status      text NOT NULL,
    comments    int DEFAULT 0,
    cost_usd    numeric(10, 4) DEFAULT 0,
    test_pass   boolean,
    duration_ms int,
    created_at  timestamptz DEFAULT now()
);
ALTER TABLE forge_reviews ENABLE ROW LEVEL SECURITY;
-- RLS via repo_id → forge_repos.tenant_id; see rules/multi-tenancy.md
```

`examples/forge/handlers/handler.py` — FastAPI sidecar:

```python
from fastapi import FastAPI, Header, HTTPException
from contextlib import asynccontextmanager
import httpx, psycopg, json, os
from psycopg.rows import dict_row

DATABASE_URL = os.environ["FORGE_DATABASE_URL"]
RUNTIME_URL  = os.environ.get("AF_STACK_URL", "http://runtime:8080")
GITHUB_WEBHOOK_SECRET = os.environ["GITHUB_WEBHOOK_SECRET"]

app = FastAPI(title="Forge API", version="0.1.0")

@app.post("/webhooks/github")
async def github_webhook(
    request: Request,
    x_hub_signature_256: str = Header(),
    x_github_event: str = Header(),
):
    # HMAC verification — the runtime's webhooks-in does this when route
    # is registered there; for sidecars we verify here.
    body = await request.body()
    verify_hmac(body, x_hub_signature_256, GITHUB_WEBHOOK_SECRET)

    pr = json.loads(body)
    if x_github_event != "pull_request" or pr["action"] not in ("opened", "synchronize"):
        return {"status": "ignored"}

    # Look up the tenant + repo config (cross-tenant — webhook is unauth)
    async with _admin_conn() as conn:
        row = await (await conn.execute(
            """SELECT id, tenant_id, focus FROM forge_repos
               WHERE installation_id = %s AND github_repo = %s""",
            (pr["installation"]["id"], pr["repository"]["full_name"]),
        )).fetchone()

    if not row:
        raise HTTPException(404, "repo not connected")

    # Enqueue the review job — tenant_id propagates from here
    async with httpx.AsyncClient(base_url=RUNTIME_URL) as runtime:
        await runtime.post(
            "/api/v1/jobs",
            json={"name": "forge.review_pr", "args": {
                "repo_id": str(row["id"]),
                "github_repo": pr["repository"]["full_name"],
                "pr_number": pr["number"],
                "pr_branch": pr["pull_request"]["head"]["ref"],
                "base_branch": pr["pull_request"]["base"]["ref"],
                "focus": row["focus"],
            }},
            headers={"x-af-stack-tenant-id": row["tenant_id"]},
        )
    return {"status": "queued"}


@app.get("/reviews")
async def list_reviews(
    x_af_stack_tenant_id: str = Header(),
    x_af_stack_user_id: str = Header(),
    limit: int = 20,
):
    tenant_id, _ = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    async with _tenant_conn(tenant_id) as conn:
        rows = await (await conn.execute(
            """SELECT fr.id, fr.pr_number, fr.status, fr.comments, fr.cost_usd,
                      fr.created_at::text, repo.github_repo
               FROM forge_reviews fr
               JOIN forge_repos repo ON fr.repo_id = repo.id
               ORDER BY fr.created_at DESC LIMIT %s""",
            (limit,),
        )).fetchall()
        return rows


@app.get("/stats")
async def stats(
    x_af_stack_tenant_id: str | None = Header(default=None),
):
    """Operator stats — cross-tenant. Use _admin_conn."""
    async with _admin_conn() as conn:
        # ... aggregate across tenants
        ...
        return {"reviews_today": ..., "cost_today_usd": ..., "top_repos": [...]}
```

The job handler (runs in the runtime, registered via
`internal/jobs/`):

```go
func ReviewPRJob(ctx context.Context, args map[string]any) error {
    result, err := suite.Agents.Call(ctx, "forge.review_pr", args)
    if err != nil { return err }

    token, _ := suite.Secrets.Get(ctx, "github_app_token")
    for _, c := range result.Comments {
        postGitHubComment(ctx, token, args["github_repo"], args["pr_number"], c)
    }
    insertReview(ctx, args["repo_id"], args["pr_number"], len(result.Comments), result.TestPass)
    return nil
}
```

**~250 lines for the workload module.**

## Surface 3 — Dashboard plugin

`apps/dashboard/plugins/forge/plugin.ts`:

```ts
import { GitPullRequest } from "lucide-react"
import { definePlugin } from "@/lib/plugins"

export default definePlugin({
  id: "forge",
  label: "Forge",
  name: "Forge",
  icon: GitPullRequest,
  iconName: "GitPullRequest",
  description: "GitHub PR reviewer status across all tenants.",
  group: "build",
  version: "0.1.0",
})
```

`apps/dashboard/plugins/forge/page.tsx`:

```tsx
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card"

export default async function ForgePage() {
  const stats = await fetch(
    `${process.env.RUNTIME_URL}/workload/forge/stats`,
    { cache: "no-store" },
  ).then(r => r.json())

  return (
    <div className="grid grid-cols-3 gap-4 p-6">
      <Card>
        <CardHeader><CardTitle>Reviews today</CardTitle></CardHeader>
        <CardContent className="text-3xl">{stats.reviews_today}</CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>Cost today</CardTitle></CardHeader>
        <CardContent className="text-3xl">${stats.cost_today_usd}</CardContent>
      </Card>
      <Card className="col-span-3">
        <CardHeader><CardTitle>Top repositories</CardTitle></CardHeader>
        <CardContent>
          <ul>{stats.top_repos.map((r: any) => <li key={r.repo}>{r.repo}: {r.count}</li>)}</ul>
        </CardContent>
      </Card>
    </div>
  )
}
```

**~30 lines.**

## Surface 4 — Customer app

`apps/customer-app/src/app/(app)/dashboard/page.tsx` (customer's view of
their reviews):

```tsx
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card"

export default async function CustomerDashboard() {
  const reviews = await fetch(
    `${process.env.NEXT_PUBLIC_RUNTIME_URL}/workload/forge/reviews?limit=20`,
    { cache: "no-store", credentials: "include" },
  ).then(r => r.json())

  return (
    <div className="p-6 space-y-6">
      <Card>
        <CardHeader><CardTitle>Your reviews this month</CardTitle></CardHeader>
        <CardContent>
          <table>
            <tbody>
              {reviews.map((r: any) => (
                <tr key={r.id}>
                  <td>{r.github_repo} #{r.pr_number}</td>
                  <td>{r.comments} comments</td>
                  <td>${r.cost_usd}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  )
}
```

Plus a `(app)/repos/page.tsx` to connect / disconnect repos, and the
existing `(app)/billing/page.tsx` (pre-wired) for Stripe/Lago.

**~3–5 customer-app pages, ~300 lines total.**

## Deploy

```bash
# Day 1
git clone github.com/yourorg/forge   # their fork of AF Stack
cd forge
af-stack init --name "Forge" --color "#0066FF" --logo ./forge-logo.svg

# Day 2–4: write the agent + workload module + dashboard plugin
#          + tweak 4 customer-app pages

# Day 5
cp .env.example .env
# edit: OPENROUTER_API_KEY, GITHUB_APP_PRIVATE_KEY, STRIPE_SECRET_KEY,
#       GITHUB_WEBHOOK_SECRET
docker compose up         # local end-to-end test

# Day 6–7
af-stack deploy fly      # production at forge.yourbrand.com
                          # GitHub App at github.com/apps/forge-by-yourbrand
                          # Operator console at admin.forge.yourbrand.com
                          # API at api.forge.yourbrand.com
```

## What this demonstrates

| Primitive | How Forge uses it |
|---|---|
| Webhooks IN (HMAC) | GitHub webhook signature verified |
| Multi-tenancy (RLS) | `forge_repos` + `forge_reviews` scoped per customer |
| Jobs (River) | Webhook returns 202 immediately; review runs async |
| Agents (AgentField) | The `review_pr` reasoner |
| Harnesses | `claude-code` runs inside the sandbox |
| Sandboxes | Git checkout + npm test isolated |
| Secrets vault | GitHub App private key + OAuth tokens |
| Cost ledger | Per-PR cost on the customer's tenant |
| Billing | Per-seat pricing via Stripe/Lago |
| Customer App | Branded SaaS the customer logs into |
| Dashboard Plugin | Cross-tenant operator view |

**Pattern**: reactive single-shot agent. Webhook → job → agent → sandbox →
result → DB + callback.

## Variations of this pattern

Any "respond to an external event with an agent" app uses the same
shape:
- Stripe webhook → fraud-detection agent
- Slack message → support-triage agent
- Calendar event → meeting-prep agent
- New customer signup → onboarding-agent

Swap the webhook source, swap the agent reasoner, keep everything else.
