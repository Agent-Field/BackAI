# Add your own agent

A 20-minute walkthrough that takes you from `cp -r` to a working custom
reasoner that the Notable handler can invoke, the billing module can
cost-track, and the smoke test can verify.

We will build `extract_action_items` — a reasoner that pulls action
items out of a meeting note. Same shape as the three that ship, just
yours.

Prerequisites: the Notable example is running (`docker compose up`) and
the seed script has been run at least once.

---

## 1. Scaffold the reasoner (~2 min)

Copy `summarize` as a starting point. It is the smallest of the three
and closest in shape to what we want.

```bash
cd examples/01-notable
cp -r agents/summarize agents/extract_action_items
```

Open `agents/extract_action_items/main.py` and replace its contents
with the new reasoner. Three things change vs. the source:

1. The Pydantic schema's shape.
2. The system prompt.
3. The function name.

```python
"""extract_action_items — pull explicit and implicit action items from a note."""

from __future__ import annotations
from typing import Any
from pydantic import BaseModel, Field

from main import app


class ActionItem(BaseModel):
    text: str
    owner: str | None = Field(
        default=None,
        description="Person responsible if the note names one; null otherwise.",
    )


class ActionItems(BaseModel):
    items: list[ActionItem] = Field(default_factory=list, max_length=20)


@app.reasoner(tags=["notable", "action-items"])
async def extract_action_items(payload: dict[str, Any]) -> dict[str, Any]:
    body = (payload.get("body") or "").strip()
    if not body:
        return {"items": []}

    result: ActionItems = await app.ai(
        system=(
            "Extract action items from the user's note. Include both "
            "explicit TODOs and implicit commitments ('I'll send the "
            "deck tomorrow'). Set owner only when the note names one."
        ),
        user=body,
        schema=ActionItems,
    )
    return result.model_dump()
```

## 2. Register it (~30s)

Open `agents/main.py` and add one import below the existing three:

```python
from extract_action_items import main as _extract_action_items  # noqa: E402,F401
```

That is the entire registration step. AgentField sees the decorator on
the function and registers `notable-ai.extract_action_items` when the
process boots.

## 3. Test it from the agent layer (~2 min)

Restart the agents container so the new reasoner registers:

```bash
docker compose up -d --build notable-agents
```

Confirm AgentField picked it up:

```bash
curl http://localhost:8081/nodes | jq '.[] | select(.id == "notable-ai") | .reasoners'
# Should now include "extract_action_items".
```

Hit it directly through the runtime gateway (which is how the handler
will call it):

```bash
curl -sS http://localhost:8080/agents/notable-ai.extract_action_items \
  -H 'content-type: application/json' \
  -H 'x-af-stack-tenant-id: acme' \
  -d '{
    "input": {
      "body": "Talked to Sarah about the Q4 roadmap. I will send her the OKR draft Friday. We agreed to push the analytics work to Q1."
    }
  }' | jq
```

You should get something like:

```json
{
  "output": {
    "items": [
      { "text": "Send Sarah the OKR draft on Friday", "owner": "me" },
      { "text": "Push analytics work to Q1", "owner": null }
    ]
  }
}
```

## 4. Wire it into the handler (~5 min)

Open `handlers/notes.py`. The pattern is the same as
`summarize_note`: read the row, call the agent, persist or return.
Add this route near the other agent endpoints:

```python
@app.patch("/notable/notes/{note_id}/extract-action-items")
async def extract_action_items_for_note(
    note_id: str,
    x_af_stack_tenant_id: Optional[str] = Header(default=None),
    x_af_stack_user_id: Optional[str] = Header(default=None),
) -> dict[str, Any]:
    tenant_id, _ = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    note = await get_note(note_id, x_af_stack_tenant_id, x_af_stack_user_id)

    result = await _call_agent(
        "notable-ai.extract_action_items",
        {"body": note.body},
        tenant_id=tenant_id,
    )
    # Action items are not stored on the row (yet). We just return them.
    # If you want them persisted, add an `action_items jsonb` column in
    # a new migration and update here.
    return {"note_id": note.id, "items": (result or {}).get("items") or []}
```

Rebuild and restart the API:

```bash
docker compose up -d --build notable-api
```

Test it end-to-end:

```bash
# Grab the id of an existing acme note.
NOTE_ID=$(curl -sS http://localhost:8090/notable/notes \
  -H 'x-af-stack-tenant-id: acme' \
  -H 'x-af-stack-user-id: alice' \
  | jq -r '.notes[0].id')

curl -sS -X PATCH "http://localhost:8090/notable/notes/${NOTE_ID}/extract-action-items" \
  -H 'x-af-stack-tenant-id: acme' \
  -H 'x-af-stack-user-id: alice' \
  | jq
```

## 5. Cost tracking — already on (~0 min)

You did nothing for this. Every `app.ai()` call from your reasoner
flows through the LLM gateway, the gateway fires the post-call hook,
and the billing module aggregates the cost into the tenant's monthly
meter row.

Check it:

```bash
curl -sS 'http://localhost:8080/api/v1/billing/meters?tenant=acme' | jq
```

You'll see your reasoner's spend rolled into the same meters the
shipped three populate.

## 6. (Optional) Add a custom usage meter (~3 min)

If you want a counter that fires once per call — independent of token
cost — add one line to your handler:

```python
await _meter("notable_action_items_extracted", 1, tenant_id)
```

It will show up in `GET /api/v1/billing/meters` immediately. The
dashboard plugin already iterates every meter for a tenant, so the new
counter appears in the Notable plugin page on the next reload.

## 7. (Optional) Update the smoke test (~3 min)

Add an assertion to `scripts/smoke-test.sh`:

```bash
info "PATCH /notable/notes/${NOTE_ID}/extract-action-items"
EAI_JSON=$(curl -fsS -X PATCH "${API_URL}/notable/notes/${NOTE_ID}/extract-action-items" \
  -H "x-af-stack-tenant-id: ${TENANT}" \
  -H "x-af-stack-user-id: ${USER}")
ITEM_COUNT=$(echo "$EAI_JSON" | jq '.items | length')
[[ "$ITEM_COUNT" -ge 1 ]] || fail "extract_action_items found no items"
ok "extract_action_items returned ${ITEM_COUNT} items"
```

Run it:

```bash
./scripts/smoke-test.sh
```

If it goes green, your new reasoner is production-shaped: real schema,
real tenancy, real cost tracking, real test coverage.

---

## What you did *not* have to think about

- Service-to-service auth — `x-af-stack-tenant-id` flows on its own.
- Provider failover — the model selection logic in `agents/main.py`
  already picks the cheapest available provider.
- Cost attribution — the gateway tags every token by tenant.
- Storage of the result — your reasoner returns a Pydantic-typed JSON
  object; the handler decides whether to persist it.
- Dashboard visibility — meters bubble up to `/api/v1/billing/meters`
  automatically, and the Notable plugin already renders them.

That's roughly the size of the gap between "I want a new AI feature"
and "it's live for every tenant on every plan, cost-tracked." Twenty
minutes if you don't get fancy.
