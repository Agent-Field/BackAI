# SPDX-License-Identifier: Apache-2.0

"""``suite.memory.*`` — scoped key/value + semantic memory primitives.

Maps to the REST surface defined in ``apps/dashboard/src/lib/api.ts`` under
the Phase 8 Memory section:

==================================  ====================================
SDK call                            Endpoint
==================================  ====================================
:func:`get`                         ``GET    /api/v1/memory/get``
:func:`put`                         ``PUT    /api/v1/memory``
:func:`delete`                      ``DELETE /api/v1/memory``
:func:`list`                        ``GET    /api/v1/memory``
:func:`search`                      ``POST   /api/v1/memory/search``
==================================  ====================================

Memory is multi-scoped: each entry lives under a ``scope`` (``global``,
``tenant``, ``agent``, ``session``, or ``run``) and an optional
``scope_id`` (e.g. a tenant id, agent name, session token). Values are
opaque JSON; setting ``embed=True`` on :func:`put` asks the runtime to
compute an embedding so the entry shows up in :func:`search` results.

The :class:`MemoryEntry`, :class:`MemoryList`, :class:`MemorySearchHit`,
and :class:`MemorySearchResult` Pydantic models mirror the zod schemas in
``lib/api.ts`` exactly — keep them in sync when the contract changes.
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from . import _http

MemoryScope = Literal["global", "tenant", "agent", "session", "run"]

_ALLOWED_SCOPES: tuple[str, ...] = ("global", "tenant", "agent", "session", "run")


class MemoryEntry(BaseModel):
    """One memory row.

    Mirrors ``MemoryEntrySchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    scope: MemoryScope
    scope_id: str | None = None
    key: str
    value: Any = None
    metadata: dict[str, Any] = Field(default_factory=dict)
    has_embedding: bool = False
    created_at: str
    updated_at: str


class MemoryList(BaseModel):
    """Paginated list of memory entries.

    Mirrors ``MemoryListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    entries: list[MemoryEntry] = Field(default_factory=list)
    total: int = 0
    has_more: bool = False


class MemorySearchHit(BaseModel):
    """One similarity-search hit.

    Mirrors ``MemorySearchHitSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    entry: MemoryEntry
    similarity: float
    distance: float


class MemorySearchResult(BaseModel):
    """Top-k similarity-search response.

    Mirrors ``MemorySearchResultSchema`` in ``apps/dashboard/src/lib/api.ts``.
    Hits arrive in descending similarity order.
    """

    model_config = ConfigDict(extra="allow")

    hits: list[MemorySearchHit] = Field(default_factory=list)
    duration_ms: int = 0


async def get(
    scope: str,
    key: str,
    scope_id: str | None = None,
) -> MemoryEntry:
    """Fetch a single memory entry by ``(scope, scope_id, key)``.

    ``scope_id`` is required for any non-``global`` scope in practice —
    the runtime enforces the rule and returns a structured error if it's
    missing. The SDK forwards what the caller supplies without local
    coercion so future scope semantics keep working.
    """
    _validate_scope(scope)
    if not key:
        raise ValueError("memory key must be a non-empty string")
    params: dict[str, Any] = {"scope": scope, "key": key}
    if scope_id is not None:
        params["scope_id"] = scope_id
    body = await _http.request_json("GET", "/memory/get", params=params)
    return MemoryEntry.model_validate(body or {})


async def put(
    scope: str,
    key: str,
    value: Any,
    *,
    scope_id: str | None = None,
    metadata: dict[str, Any] | None = None,
    embed: bool = False,
) -> MemoryEntry:
    """Upsert a memory entry and return the persisted row.

    ``value`` is forwarded verbatim — it can be any JSON-serialisable
    payload (string, number, list, dict, nested structures). Setting
    ``embed=True`` instructs the runtime to compute an embedding using
    the configured embedding model so the entry becomes searchable via
    :func:`search`.
    """
    _validate_scope(scope)
    if not key:
        raise ValueError("memory key must be a non-empty string")
    payload: dict[str, Any] = {
        "scope": scope,
        "key": key,
        "value": value,
        "embed": bool(embed),
    }
    if scope_id is not None:
        payload["scope_id"] = scope_id
    if metadata is not None:
        payload["metadata"] = metadata
    body = await _http.request_json("PUT", "/memory", json=payload)
    return MemoryEntry.model_validate(body or {})


async def delete(
    scope: str,
    key: str,
    scope_id: str | None = None,
) -> bool:
    """Delete a memory entry. Returns ``True`` if the row was removed."""
    _validate_scope(scope)
    if not key:
        raise ValueError("memory key must be a non-empty string")
    params: dict[str, Any] = {"scope": scope, "key": key}
    if scope_id is not None:
        params["scope_id"] = scope_id
    body = await _http.request_json("DELETE", "/memory", params=params)
    if not isinstance(body, dict):
        return True
    return bool(body.get("deleted", True))


async def list(  # noqa: A001 — mirrors the JS `memory.list()` ergonomics
    *,
    scope: str | None = None,
    scope_id: str | None = None,
    prefix: str | None = None,
    limit: int = 100,
    offset: int = 0,
) -> MemoryList:
    """List memory entries with optional filters.

    All filters are forwarded as query parameters. ``prefix`` performs a
    server-side key-prefix match (e.g. ``"user:"``). When neither
    ``scope`` nor ``scope_id`` is supplied, the runtime returns entries
    visible to the calling identity.
    """
    if limit <= 0:
        raise ValueError("limit must be a positive integer")
    if offset < 0:
        raise ValueError("offset must be non-negative")
    if scope is not None:
        _validate_scope(scope)
    params: dict[str, Any] = {"limit": limit, "offset": offset}
    if scope is not None:
        params["scope"] = scope
    if scope_id is not None:
        params["scope_id"] = scope_id
    if prefix is not None:
        params["prefix"] = prefix
    body = await _http.request_json("GET", "/memory", params=params)
    return MemoryList.model_validate(body or {"entries": [], "total": 0, "has_more": False})


async def search(
    query: str,
    *,
    scope: str | None = None,
    scope_id: str | None = None,
    top_k: int = 10,
    threshold: float | None = None,
) -> MemorySearchResult:
    """Semantic search across embedded memory entries.

    ``query`` is embedded server-side and matched against entries written
    with ``embed=True``. Results are returned in descending similarity
    order. ``threshold`` (0–1) optionally drops hits below a minimum
    similarity score.
    """
    if not query:
        raise ValueError("memory search query must be a non-empty string")
    if top_k < 1 or top_k > 50:
        raise ValueError("top_k must be between 1 and 50")
    if threshold is not None and (threshold < 0 or threshold > 1):
        raise ValueError("threshold must be between 0 and 1")
    if scope is not None:
        _validate_scope(scope)
    payload: dict[str, Any] = {"query": query, "top_k": top_k}
    if scope is not None:
        payload["scope"] = scope
    if scope_id is not None:
        payload["scope_id"] = scope_id
    if threshold is not None:
        payload["threshold"] = threshold
    body = await _http.request_json("POST", "/memory/search", json=payload)
    return MemorySearchResult.model_validate(body or {"hits": [], "duration_ms": 0})


def _validate_scope(scope: str) -> None:
    if scope not in _ALLOWED_SCOPES:
        allowed = ", ".join(_ALLOWED_SCOPES)
        raise ValueError(f"scope must be one of: {allowed}; got {scope!r}")


__all__ = [
    "MemoryEntry",
    "MemoryList",
    "MemoryScope",
    "MemorySearchHit",
    "MemorySearchResult",
    "delete",
    "get",
    "list",
    "put",
    "search",
]
