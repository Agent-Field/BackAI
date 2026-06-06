# Example walk: Notable (Notion-with-AI)

Validation workload #1: the **pure-app** case. CRUD-heavy, multi-user,
collaboration-friendly, AI as a feature among many.

## Product

Workspaces · pages with rich text · real-time collab · AI features
(summarize, suggest related, agentic to-do completion) · Stripe billing ·
search across pages.

## Modules enabled

| Module | State | Adapter |
|---|---|---|
| identity | on | better-auth |
| multi-tenancy | on | PG RLS (workspace = tenant) |
| public-gateway | on | suite native |
| llm-gateway | on | LiteLLM via AF |
| jobs | on | River |
| crons | on | River cron |
| storage | on | MinIO (dev) → S3 (prod) |
| secrets-vault | on | PG + KMS |
| notifications | on | log-stub (dev) → Resend (prod) |
| webhooks-in | on | Svix |
| billing | on | Stripe direct |
| search | on | Postgres FTS |
| dashboard | on, customized | shadcn + Tremor |

Disabled: sandbox, git-workload, multimodal-storage, change-stream-listener.

13/16 modules on. Solid coverage without bloat.

## Repo structure

```
notable/
  docker-compose.yml
  apps/backend/
    agents/notable-ai/
      summarizer.py
      related_suggester.py
      todo_completer.py
    handlers/
      pages.py
      workspaces.py
      uploads.py
    jobs/
      send_mention_email.py
      reindex_page.py
      process_upload.py
    crons/
      daily_digest.py
    migrations/
    config.yaml
  apps/dashboard/
    app/(workspace)/[slug]/pages/   # custom product UI
    app/(admin)/                     # operator views
```

## End-to-end flow: "Auto-complete to-dos"

1. User clicks button → `POST /api/v1/agents/notable-ai.todo_completer`
2. **public-gateway** authenticates session, resolves tenant, rate-limits
3. **AF** receives the call, starts execution
4. Agent calls `app.harness(...)` with read-only tools (no sandbox needed)
5. Reads page content, vector-searches related pages via `app.memory.search()`
6. Returns suggested completions
7. **llm-gateway** logged tokens used → cost charged to tenant
8. **billing** aggregates cost into tenant counter
9. Response returned to frontend
10. **notifications** fires email "Your to-dos are complete"

Touches: gateway → AF → memory → llm-gateway → billing → notifications.
Five modules. The dev didn't think about any of them.

## Code samples

**AF agent:**
```python
from agentfield import Agent

app = Agent(node_id="notable-ai")

@app.reasoner()
async def summarize_page(page: dict) -> dict:
    result = await app.ai(
        system="Summarize concisely.",
        user=page["content"],
        schema=Summary,
    )
    return result.model_dump()
```

**Plain handler (non-agent):**
```python
@handler.post("/api/v1/pages")
async def create_page(body: dict):
    page = await db.pages.insert({
        "tenant_id": ctx.tenant_id,           # auto from MT middleware
        "title": body["title"],
        "content": body["content"],
    })
    await suite.jobs.enqueue("reindex_page", {"page_id": page.id})
    return page
```

**Calling the summarize agent from the frontend:**

All LLM usage routes through AF. For one-shot calls like "summarize this," the
dev writes a small AF reasoner once and calls it from anywhere.

```python
# apps/backend/agents/notable-ai/summarizer.py
@app.reasoner()
async def summarize_page(page: dict) -> dict:
    result = await app.ai(system="Summarize concisely.", user=page["content"])
    return {"summary": result}
```

```tsx
// From the frontend (or any non-agent code)
const result = await suite.agents.call("notable-ai.summarize_page", {
  content: pageContent,
})
```

Same AF execution path as any other agent call. Cost is attributed to the
tenant. Trace shows up in the dashboard. Identity is enforced.

If the dev really wants OpenAI-format calls (existing tooling like Vercel AI
SDK), they point it at `/api/v1/llm/chat/completions` — that endpoint exists
but routes through AF internally too.

## What dev did NOT have to build

- Login, signup, password reset (better-auth ships it)
- Member invites, role management (MT module ships it)
- Billing portal embed (Stripe direct adapter ships it)
- API key issuance (gateway module ships it)
- Account settings page (scaffold ships it)
- Job queue, retries, dead-letter (River)
- LLM gateway, cost tracking, caching (suite gateway)
- Search infrastructure (PG FTS)

## What dev DID build

- Pages CRUD UI
- Real-time collab layer (Yjs or similar, their choice)
- The three custom AF agents
- Custom AI feature buttons

Rough split: ~70% suite primitives, ~30% product-specific. The viral
promise concretized.

## Pain points

1. Real-time collab is dev's problem; suite has pub/sub but CRDT is not
   bundled. Document Yjs/Liveblocks/PartyKit patterns.
2. PG FTS lacks typo tolerance. Document one-line Meilisearch swap.
3. Dashboard is Next.js. If dev wants Svelte for product UI, they run two
   apps. Document this honestly.

None are blockers.
