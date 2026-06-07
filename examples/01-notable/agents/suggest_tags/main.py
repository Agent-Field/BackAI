"""suggest_tags — propose up to 5 tags for a note, biased by user history.

Demonstrates the AF Stack ``memory`` primitive: we read the user's
previously-used tags from ``scope=agent, scope_id=<tenant>:<user>`` so
the model's suggestions stay consistent with how the user already files
notes ("ml" not "machine-learning", "ops" not "devops"), then we write
the merged list back so the bias compounds over time.

Why we don't use ``scope=user`` directly: the memory module's
``scope=user`` is keyed by AF Stack identity, which only exists for
logged-in dashboard sessions. Agents called from server-side handlers
arrive with a tenant + user *id* but no session token, so we use the
agent scope with a composite ``tenant:user`` key. The data shape is the
same; only the namespace differs.
"""

from __future__ import annotations

import os
from typing import Any

import httpx
from pydantic import BaseModel, Field

from main import app


class TagSuggestions(BaseModel):
    """Bounded list — 5 tags is the cap callers should display."""

    tags: list[str] = Field(
        default_factory=list,
        description="Up to 5 short, lowercase, kebab-or-snake tags.",
        max_length=5,
    )


# Base URL for the runtime — same env name the SDK uses so the agent
# inherits whatever the operator already configured.
_RUNTIME_URL = os.getenv("AF_STACK_URL", "http://runtime:8080").rstrip("/")
_MEMORY_KEY = "note-tags"


async def _load_user_tags(tenant_id: str, user_id: str) -> list[str]:
    """Pull the user's previously-used tags from memory. Returns [] on miss.

    We deliberately swallow non-200s here: a memory outage shouldn't
    block tag suggestions, it should just degrade them to the
    history-free flavour.
    """
    if not tenant_id or not user_id:
        return []
    scope_id = f"{tenant_id}:{user_id}"
    url = f"{_RUNTIME_URL}/api/v1/memory/get"
    headers = {"x-af-stack-tenant-id": tenant_id}
    try:
        async with httpx.AsyncClient(timeout=5.0) as client:
            resp = await client.get(
                url,
                params={"scope": "agent", "scope_id": scope_id, "key": _MEMORY_KEY},
                headers=headers,
            )
        if resp.status_code != 200:
            return []
        value = (resp.json() or {}).get("value") or []
        # Be defensive: memory values are opaque JSON; we only want a
        # list of strings here.
        if isinstance(value, list):
            return [str(t).strip() for t in value if isinstance(t, str)]
        return []
    except (httpx.RequestError, ValueError):
        return []


async def _save_user_tags(tenant_id: str, user_id: str, tags: list[str]) -> None:
    """Upsert the merged tag list. Fire-and-forget — memory writes are
    not in the critical path for the response."""
    if not tenant_id or not user_id or not tags:
        return
    scope_id = f"{tenant_id}:{user_id}"
    url = f"{_RUNTIME_URL}/api/v1/memory"
    headers = {"x-af-stack-tenant-id": tenant_id}
    payload = {
        "scope": "agent",
        "scope_id": scope_id,
        "key": _MEMORY_KEY,
        "value": tags,
        # No embed — we look this up by exact key, not via search.
        "embed": False,
    }
    try:
        async with httpx.AsyncClient(timeout=5.0) as client:
            await client.put(url, json=payload, headers=headers)
    except httpx.RequestError:
        # Memory's a nice-to-have for this reasoner; never fail the call.
        return


@app.reasoner(tags=["notable", "tags", "memory"])
async def suggest_tags(payload: dict[str, Any]) -> dict[str, Any]:
    """Suggest tags for ``payload['body']`` biased by the user's history.

    Input  : ``{body: str, user_id: str, tenant_id: str}``
    Output : ``{tags: list[str]}`` — at most 5 lowercase tags.
    """
    body = (payload.get("body") or "").strip()
    user_id = payload.get("user_id") or ""
    tenant_id = payload.get("tenant_id") or ""
    if not body:
        return {"tags": []}

    history = await _load_user_tags(tenant_id, user_id)
    history_hint = ""
    if history:
        # We surface history as a soft preference, not a hard constraint.
        # That way the model can still propose net-new tags when the
        # note genuinely covers ground the user hasn't filed before.
        history_hint = (
            "\n\nThe user has previously used these tags — prefer reusing "
            "them when the note matches:\n- " + "\n- ".join(history[:25])
        )

    result: TagSuggestions = await app.ai(
        system=(
            "Propose up to 5 short tags for the user's note. Tags must be "
            "lowercase, single token (kebab-or-snake-case allowed), and "
            "describe the topic or domain. Never invent personal names."
            + history_hint
        ),
        user=body,
        schema=TagSuggestions,
    )
    tags = [t.lower().strip() for t in result.tags if t and t.strip()][:5]

    # Merge: union of history + new, preserving the new tags' order so
    # the freshest signal stays at the top of the list.
    merged: list[str] = []
    seen: set[str] = set()
    for t in tags + history:
        if t not in seen:
            merged.append(t)
            seen.add(t)
    await _save_user_tags(tenant_id, user_id, merged[:50])  # cap to keep memory tight

    return {"tags": tags}
