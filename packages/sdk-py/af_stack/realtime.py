# SPDX-License-Identifier: Apache-2.0

"""``suite.realtime.*`` — Postgres LISTEN/NOTIFY backed realtime subscriptions.

Maps to the WebSocket endpoint in
``services/runtime/internal/server/realtime.go``:

==================================  ===================================
SDK call                            Endpoint
==================================  ===================================
:func:`subscribe`                   ``WS  /api/v1/realtime``
==================================  ===================================

The runtime upgrades the HTTP request to a WebSocket and forwards
JSON-encoded ``NOTIFY`` payloads from the ``suite_realtime`` Postgres
channel. Filtering on table, tenant, and a JSON ``filter`` happens
server-side so an idle client receives only matching events.

Caveats (Phase 2 — server-side is shipping):

* The runtime returns 503 ``REALTIME_NOT_CONFIGURED`` when no database is
  wired. The async iterator raises :class:`af_stack._http.AFStackError`
  in that case.
* This module depends on the optional ``websockets`` package (already a
  transitive dep of ``httpx``'s ecosystem on most installs). When it's
  missing, :func:`subscribe` raises a clear :class:`RuntimeError` with
  install instructions rather than failing at import time.
* The TypeScript SDK returns a raw ``WebSocket`` object so the caller
  drives event dispatch. The Python SDK returns an async iterator so
  callers can use ``async for evt in suite.realtime.subscribe(...)`` —
  closer to Python's normal idiom. Pass a cancellation token via
  ``asyncio.CancelledError`` (cancel the surrounding task) to stop.

The :class:`RealtimeEvent` model mirrors the payload shape emitted by
``handleRealtime`` (table / op / tenant_id / record / old / at).
"""

from __future__ import annotations

import json
import os
from collections.abc import AsyncIterator
from typing import Any
from urllib.parse import urlencode, urlsplit, urlunsplit

from pydantic import BaseModel, ConfigDict, Field

from . import _http

# Filter values must be primitive JSON scalars so the wire payload stays
# round-trip-stable; nested objects/arrays would need a richer match
# language (which the runtime does not yet implement).
RealtimeFilterValue = str | int | float | bool | None
RealtimeFilter = dict[str, RealtimeFilterValue]


class RealtimeEvent(BaseModel):
    """One realtime event delivered over the WebSocket.

    Mirrors the JSON payload emitted by ``handleRealtime`` in
    ``services/runtime/internal/server/realtime.go`` and the
    ``RealtimeEvent`` interface in ``packages/sdk-ts/src/realtime.ts``.
    """

    model_config = ConfigDict(extra="allow")

    table: str
    op: str | None = None
    tenant_id: str | None = None
    record: dict[str, Any] | None = None
    old: dict[str, Any] | None = None
    at: str | None = None
    raw: dict[str, Any] = Field(default_factory=dict)


def _resolve_base_url() -> str:
    return os.environ.get("AF_STACK_URL", _http.DEFAULT_BASE_URL).rstrip("/")


def _resolve_api_key() -> str | None:
    key = os.environ.get("AF_STACK_API_KEY")
    return key if key else None


def _to_ws_base(base_url: str) -> str:
    parts = urlsplit(base_url)
    scheme = "wss" if parts.scheme == "https" else "ws"
    # Strip any trailing slash from the path so the join is exact.
    path = parts.path.rstrip("/")
    return urlunsplit((scheme, parts.netloc, path, "", ""))


def _build_ws_url(table: str, rt_filter: RealtimeFilter) -> str:
    base = _to_ws_base(_resolve_base_url())
    params: list[tuple[str, str]] = [("table", table)]
    if rt_filter:
        params.append(("filter", json.dumps(rt_filter, separators=(",", ":"))))
    api_key = _resolve_api_key()
    if api_key is not None:
        params.append(("api_key", api_key))
    return f"{base}{_http.API_PREFIX}/realtime?{urlencode(params)}"


def subscribe(
    table: str,
    rt_filter: RealtimeFilter | None = None,
) -> AsyncIterator[RealtimeEvent]:
    """Subscribe to realtime ``NOTIFY`` events for ``table``.

    Returns an async iterator yielding :class:`RealtimeEvent` objects as
    they arrive. Iteration ends when the runtime closes the socket (e.g.
    on shutdown). Cancel the surrounding task to stop early — the
    underlying WebSocket is closed when the async generator is finalised.

    ``rt_filter`` is a JSON object of scalar-equality predicates matched
    server-side against each event's ``record`` map. Filtering happens on
    the runtime, not the client, so idle subscriptions stay cheap.

    Returned as a sync function (despite being async-iterable) so callers
    don't need ``await`` before iterating::

        async for evt in suite.realtime.subscribe("notes"):
            ...

    The first ``__anext__`` opens the WebSocket; argument validation
    happens up-front so input errors raise immediately.

    Raises :class:`RuntimeError` (on first iteration) if the optional
    ``websockets`` package is not installed. Raises
    :class:`af_stack._http.AFStackError` (on first iteration) if the
    runtime rejects the subscription (e.g. ``REALTIME_NOT_CONFIGURED``).
    """
    if not isinstance(table, str) or table.strip() == "":
        raise ValueError("table is required")

    url = _build_ws_url(table, dict(rt_filter or {}))
    return _stream_events(url)


async def _stream_events(url: str) -> AsyncIterator[RealtimeEvent]:
    """Open the WebSocket and yield parsed events until the socket closes.

    Split out so :func:`subscribe` can do argument validation up-front
    while the actual generator only runs once the caller iterates.
    """
    # Lazy import so the SDK still imports without ``websockets`` available.
    # ``websockets`` is a small pure-Python dep; we want a clear error
    # rather than crashing at SDK import time on minimal installs.
    try:
        from websockets.asyncio.client import connect as ws_connect
    except ImportError as exc:  # pragma: no cover — surfaced verbatim to caller
        raise RuntimeError(
            "suite.realtime.subscribe requires the `websockets` package. "
            "Install with: pip install websockets"
        ) from exc

    async with ws_connect(url) as socket:
        async for raw in socket:
            if isinstance(raw, bytes):
                try:
                    raw = raw.decode("utf-8")
                except UnicodeDecodeError:
                    continue
            if not isinstance(raw, str):
                continue
            try:
                payload = json.loads(raw)
            except json.JSONDecodeError:
                continue
            if not isinstance(payload, dict):
                continue
            yield RealtimeEvent.model_validate({**payload, "raw": payload})


__all__ = [
    "RealtimeEvent",
    "RealtimeFilter",
    "RealtimeFilterValue",
    "subscribe",
]
