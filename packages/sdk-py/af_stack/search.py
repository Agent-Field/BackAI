# SPDX-License-Identifier: Apache-2.0

"""``suite.search.*`` — tenant-scoped app-data search.

Maps to the REST surface in ``services/runtime/internal/server/search.go``:

==================================  =====================================================
SDK call                            Endpoint
==================================  =====================================================
:func:`search`                      ``POST   /api/v1/search``
:func:`upsert`                      ``PUT    /api/v1/search/documents``
:func:`delete`                      ``DELETE /api/v1/search/documents/{namespace}/{key}``
==================================  =====================================================

This surface indexes records owned by the user's product or workload
modules. It deliberately does **not** expose AgentField memory,
sessions, runs, spans, or traces — use :mod:`af_stack.memory` for
agent memory.

Three modes are supported (see :data:`SearchMode`):

* ``"fts"`` — Postgres full-text search ranking.
* ``"vector"`` — embedding similarity (requires an embedder configured
  in the LLM gateway).
* ``"hybrid"`` — both, fused. Default when ``mode`` is omitted.

The :class:`SearchDocument`, :class:`SearchHit`, and :class:`SearchResult`
Pydantic models mirror the zod schemas in ``packages/sdk-ts/src/search.ts``
(snake_case wire form, since the Python SDK keeps the wire JSON shape).
"""

from __future__ import annotations

from typing import Any, Literal
from urllib.parse import quote

from pydantic import BaseModel, ConfigDict, Field

from . import _http

SearchMode = Literal["fts", "vector", "hybrid"]

_ALLOWED_MODES: tuple[str, ...] = ("fts", "vector", "hybrid")


class SearchDocument(BaseModel):
    """One indexed document.

    Mirrors ``SearchDocumentSchema`` in ``packages/sdk-ts/src/search.ts``
    and the ``Document`` struct in
    ``services/runtime/internal/search/types.go``.
    """

    model_config = ConfigDict(extra="allow")

    tenant_id: str
    namespace: str
    key: str
    title: str = ""
    body: str = ""
    metadata: dict[str, Any] = Field(default_factory=dict)
    has_embedding: bool = False
    created_at: str
    updated_at: str


class SearchHit(BaseModel):
    """One row of a search result, including the document and ranking signals.

    Mirrors ``SearchHitSchema`` in ``packages/sdk-ts/src/search.ts``.
    """

    model_config = ConfigDict(extra="allow")

    document: SearchDocument
    score: float
    fts_rank: float | None = None
    similarity: float | None = None


class SearchResult(BaseModel):
    """Outcome of a single :func:`search` call.

    Mirrors ``SearchResultSchema`` in ``packages/sdk-ts/src/search.ts``.
    """

    model_config = ConfigDict(extra="allow")

    hits: list[SearchHit] = Field(default_factory=list)
    mode: SearchMode = "hybrid"
    duration_ms: int = 0


async def search(
    query: str,
    *,
    mode: SearchMode = "hybrid",
    namespace: str | None = None,
    metadata_filter: dict[str, Any] | None = None,
    limit: int = 10,
    offset: int = 0,
) -> SearchResult:
    """Search tenant-scoped app data.

    ``mode`` selects FTS, vector, or hybrid retrieval; default is hybrid.
    ``namespace`` scopes the search to one namespace (defaults server-side
    to ``"default"`` when omitted). ``metadata_filter`` is a JSON object
    matched against the document's ``metadata`` map. ``limit`` must be
    in ``[1, 100]``; ``offset`` must be non-negative.
    """
    if not isinstance(query, str) or query.strip() == "":
        raise ValueError("search query must be a non-empty string")
    if mode not in _ALLOWED_MODES:
        allowed = ", ".join(_ALLOWED_MODES)
        raise ValueError(f"search mode must be one of: {allowed}; got {mode!r}")
    if limit < 1 or limit > 100:
        raise ValueError("limit must be between 1 and 100")
    if offset < 0:
        raise ValueError("offset must be non-negative")

    payload: dict[str, Any] = {
        "query": query,
        "mode": mode,
        "limit": limit,
        "offset": offset,
    }
    if namespace is not None and namespace != "":
        payload["namespace"] = namespace
    if metadata_filter is not None:
        payload["metadata_filter"] = dict(metadata_filter)

    body = await _http.request_json("POST", "/search", json=payload)
    return SearchResult.model_validate(body or {})


async def upsert(
    key: str,
    *,
    namespace: str | None = None,
    title: str = "",
    body: str = "",
    metadata: dict[str, Any] | None = None,
    embed: bool = False,
) -> SearchDocument:
    """Index (or re-index) a single document.

    At least one of ``title`` or ``body`` must be non-empty. ``embed=True``
    instructs the runtime to compute an embedding via the LLM gateway —
    the call fails with ``EMBEDDER_NOT_CONFIGURED`` if no embedder is
    configured.
    """
    if not isinstance(key, str) or key == "":
        raise ValueError("search document key must be a non-empty string")
    if title.strip() == "" and body.strip() == "":
        raise ValueError("search document title or body is required")

    payload: dict[str, Any] = {
        "key": key,
        "title": title,
        "body": body,
        "embed": bool(embed),
    }
    if namespace is not None and namespace != "":
        payload["namespace"] = namespace
    if metadata is not None:
        payload["metadata"] = dict(metadata)

    raw = await _http.request_json("PUT", "/search/documents", json=payload)
    return SearchDocument.model_validate(raw or {})


async def delete(  # noqa: A001 — mirrors the JS `searchIndex.delete()` ergonomics
    key: str,
    *,
    namespace: str = "default",
) -> bool:
    """Delete an indexed document. Returns ``True`` on success."""
    if not isinstance(key, str) or key == "":
        raise ValueError("search document key must be a non-empty string")
    if not isinstance(namespace, str) or namespace == "":
        raise ValueError("search document namespace must be a non-empty string")

    path = f"/search/documents/{quote(namespace, safe='')}/{quote(key, safe='')}"
    raw = await _http.request_json("DELETE", path)
    if not isinstance(raw, dict):
        return True
    return bool(raw.get("deleted", True))


__all__ = [
    "SearchDocument",
    "SearchHit",
    "SearchMode",
    "SearchResult",
    "delete",
    "search",
    "upsert",
]
