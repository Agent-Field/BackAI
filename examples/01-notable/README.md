# Notable

Notable is what AF Stack looks like when you build a real SaaS on it.
Multi-tenant notes, three small AF agents, a billing meter, and a custom
dashboard plugin — all sitting on top of the platform's default
primitives.

It is the opinionated end-to-end example. If you want to see how
multi-tenancy, the LLM gateway, the memory store, and the billing
module fit together in one shippable thing, this is it.

## 60-second quickstart

```bash
cd examples/01-notable
cp .env.example .env
# set OPENROUTER_API_KEY in .env (cheapest path to working AI)

docker compose up -d
./scripts/seed.sh          # 2 tenants, ~5 notes each, agents run on every note
open http://localhost:3000/plugins/notable
```

The plugin shows two tenants, their note counts, and the LLM spend each
incurred while the seed ran. Click a tenant id to land on its
multi-tenancy detail page.

To prove it all still works under contract:

```bash
./scripts/smoke-test.sh
```

## Data model

One table. RLS keyed on `tenant_id`. The runtime sets
`app.tenant_id` per request and the policy reads it back.

```sql
notable_notes(
  id, tenant_id, user_id,
  title, body, tags text[], tldr,
  created_at, updated_at
)
-- INDEX (tenant_id, updated_at DESC)
-- POLICY tenant_id = current_setting('app.tenant_id')
```

That is the whole product schema. `suite_tenants`,
`suite_billing_customers`, and `suite_usage_meters` come from the
platform and Notable never touches them directly — it just consumes the
APIs.

## The three agents

All three live in `agents/` and run in one Python process
(`notable-agents` in compose). Each is a single `@app.reasoner` with a
Pydantic schema.

| Reasoner | Input | Output | What it shows |
|---|---|---|---|
| `notable-ai.summarize` | `{note_id, body}` | `{tldr, key_points}` | Smallest possible `app.ai()` with a structured schema. The handler writes the TLDR back to the row. |
| `notable-ai.suggest_tags` | `{body, user_id, tenant_id}` | `{tags: [...]}` | Reads the user's previously-used tags from the memory store, biases the suggestions toward them, writes the merged list back. Memory as a primitive, not a service. |
| `notable-ai.todo_completer` | `{body}` | `{completions: [{line, suggestion}, ...]}` | Regex extracts `- [ ]` lines (deterministic work in code, not in the LLM), then asks the model for a one-line next step per task. |

The default model is `openrouter/qwen/qwen-2.5-72b-instruct`. The full
seed pass (10 notes × summarize + suggest_tags) costs well under one
cent.

## How multi-tenancy scopes data

1. The dashboard's session middleware resolves the user to a tenant.
2. Requests to `notable-api` carry `x-af-stack-tenant-id` + `x-af-stack-user-id`.
3. The handler wraps every DB call in a transaction with `SET LOCAL app.tenant_id`.
4. The RLS policy on `notable_notes` rejects anything where the row's
   `tenant_id` doesn't match the GUC.

Net effect: a tenant cannot see another tenant's notes even if a
handler forgot to filter — the database refuses to return them.

The seed script and smoke test use `SET LOCAL app.bypass_rls = 'on'` for
the cross-tenant setup steps. That GUC is the only way out of the
policy and you should treat it like `sudo` — never set it from a user-
facing handler.

## How billing is metered

Two surfaces feed the billing module:

- **LLM tokens** are cost-tracked automatically. Every `app.ai()` call
  flows through the LLM gateway, which fires `HookLLMPostCall` after
  the upstream returns. Phase 7.2's recorder writes the cost into
  `suite_cost_events`; the billing service rolls it up into the tenant's
  monthly meter row.
- **Custom app meters** — `notable_notes_created` — fire from
  `handlers/notes.py` via `POST /api/v1/admin/billing/meter`. Every
  successful `POST /notable/notes` increments it by one.

The dashboard plugin reads both back via `GET /api/v1/billing/meters` so
you can see a tenant's note volume next to its LLM spend in the same
view.

## Repo layout

```
examples/01-notable/
  README.md
  docker-compose.yml         # standalone — extends nothing
  config.yaml                # runtime modules: MT + billing + LLM gateway + memory
  .env.example
  migrations/
    00001_notes.sql          # notable_notes + RLS + updated_at trigger
  agents/
    Dockerfile               # one image for all 3 reasoners
    requirements.txt
    main.py                  # entrypoint — imports each reasoner module
    summarize/main.py
    suggest_tags/main.py
    todo_completer/main.py
  handlers/
    Dockerfile               # FastAPI service container
    requirements.txt
    notes.py                 # /notable/notes + /notable/stats
  scripts/
    seed.sh                  # 2 tenants + stub Stripe customers + ~10 notes
    smoke-test.sh            # asserts agent contracts + billing meter
  docs/
    walkthrough.md           # "add your own agent" — 20 min walk
```

The dashboard plugin lives outside the example tree at
`apps/dashboard/plugins/notable/` because plugins are discovered at the
dashboard's build time.

## Add your own agent

The short version:

```bash
cp -r agents/summarize agents/extract_action_items
$EDITOR agents/extract_action_items/main.py
# rename the reasoner; tweak the prompt + schema

$EDITOR agents/main.py
# add: from extract_action_items import main as _extract_action_items
```

Then in `handlers/notes.py`, add a route:

```python
@app.patch("/notable/notes/{note_id}/extract-action-items")
async def extract_action_items_for(note_id: str, ...):
    note = await get_note(note_id, ...)
    result = await _call_agent(
        "notable-ai.extract_action_items",
        {"body": note.body},
        tenant_id=tenant_id,
    )
    # persist or return as you like
    return result
```

Rebuild and you're done:

```bash
docker compose up -d --build notable-agents notable-api
```

The full walkthrough — including testing, billing, and adding the
button to the dashboard — is in `docs/walkthrough.md`.

## What's deliberately out of scope

- A user-facing notes UI. The example ships server + agents + operator
  dashboard. A product UI would triple the example's size for zero
  educational value over what the operator dashboard already shows.
- Real-time collaboration. CRDTs are a product choice; AF Stack ships
  the primitives, not the editor.
- A workload-module mount for the handlers. The Phase 13.4 loader will
  let `handlers/notes.py` move under `workload-modules/notes/` and
  drop the separate container; until it lands, the FastAPI service is
  the cleanest path that still works today.
