"""``suite.cost.*`` — cost event log browsing.

Maps to the cost event REST surface in
``apps/dashboard/src/lib/api.ts``:

==================================  =========================================
SDK call                            Endpoint
==================================  =========================================
:func:`events`                      ``GET    /api/v1/cost/events``
==================================  =========================================

Each :class:`CostEvent` is one billable LLM call written by the cost
recorder (Phase 7.2). The dashboard renders these rows in the live cost
log, and a :class:`CostEventList` envelopes the paginated response.
"""

from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, ConfigDict, Field

from . import _http


class CostEvent(BaseModel):
    """One billable LLM call.

    Mirrors ``CostEventSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    id: str
    tenant_id: str | None = None
    api_key_id: str | None = None
    model: str
    provider: str
    agent: str | None = None
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int
    cost_usd: float
    cached: bool
    latency_ms: int
    occurred_at: str


class CostEventList(BaseModel):
    """Paginated list of cost events.

    Mirrors ``CostEventListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    events: list[CostEvent] = Field(default_factory=list)
    total: int = 0
    has_more: bool = False


async def events(
    *,
    tenant: str | None = None,
    model: str | None = None,
    from_: str | datetime | None = None,
    to: str | datetime | None = None,
    limit: int = 100,
    offset: int = 0,
) -> CostEventList:
    """List cost events with optional filters.

    ``from_`` and ``to`` accept RFC3339 strings or :class:`datetime`
    instances. The trailing-underscore name avoids a clash with the
    ``from`` keyword; the wire param is plain ``from``.
    """
    if limit <= 0:
        raise ValueError("limit must be a positive integer")
    if offset < 0:
        raise ValueError("offset must be non-negative")
    params: dict[str, Any] = {"limit": limit, "offset": offset}
    if tenant:
        params["tenant"] = tenant
    if model:
        params["model"] = model
    if from_ is not None:
        params["from"] = _to_iso(from_)
    if to is not None:
        params["to"] = _to_iso(to)
    body = await _http.request_json("GET", "/cost/events", params=params)
    return CostEventList.model_validate(
        body or {"events": [], "total": 0, "has_more": False}
    )


def _to_iso(value: str | datetime) -> str:
    """Serialise a :class:`datetime` to RFC3339; pass strings through.

    Naive datetimes are treated as UTC for predictability — same
    convention used by :mod:`af_stack.jobs` and :mod:`af_stack.admin.audit`.
    """
    if isinstance(value, datetime):
        iso = value.isoformat()
        if value.tzinfo is None:
            return iso + "Z"
        return iso
    return value


__all__ = ["CostEvent", "CostEventList", "events"]
