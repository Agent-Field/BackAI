# SPDX-License-Identifier: Apache-2.0

"""``suite.runs.*`` — live agent run subscriptions.

Maps to the WebSocket endpoint in
``services/runtime/internal/server/runs.go`` (item #15):

==================================  =======================================
SDK call                            Endpoint
==================================  =======================================
:func:`subscribe`                   ``WS  /api/v1/realtime/runs``
==================================  =======================================

The runtime upgrades the HTTP request to a WebSocket and forwards live
AgentField run events (started / step / completed / error) as JSON
messages. The server-side filter parameters (``tenant_id``, ``user_id``,
``agent``, ``run_id``, ``execution_id``) all happen on the runtime, not
the client — an idle subscription stays cheap.

This is the customer-app primitive used to render "agent is doing X"
UIs without polling. Compare to :mod:`af_stack.realtime`, which streams
Postgres NOTIFY events — useful for table-shaped data; this module is
for agent semantics.

Caveats:

* Lazy-imports the optional ``websockets`` package, same as
  :mod:`af_stack.realtime`. The first call to ``async for evt in
  subscribe(...)`` raises a clear :class:`RuntimeError` with install
  instructions if it isn't available.
* When AgentField is unreachable the runtime emits a single
  ``{"type": "server.degraded", "reason": ...}`` message and closes
  the socket. Callers should treat that as a transient failure and
  reconnect with backoff.
* When ``execution_id`` is supplied for a run that already finished,
  the runtime emits the cached final state then closes. This matches
  the "subscribe lazily and still see what happened" contract.
"""

from __future__ import annotations

import json
import os
from collections.abc import AsyncIterator
from typing import Any, Literal
from urllib.parse import urlencode, urlsplit, urlunsplit

from pydantic import BaseModel, ConfigDict, Field

from . import _http

# Wire-protocol event types as a literal. Matches the runEventWireMessage
# Type field in services/runtime/internal/server/runs.go.
RunEventType = Literal[
    "run.started",
    "run.step",
    "run.completed",
    "run.error",
    "server.degraded",
]


class RunEvent(BaseModel):
    """One live run event delivered over the WebSocket.

    Mirrors the JSON envelope emitted by ``handleRunsSubscribe`` in
    ``services/runtime/internal/server/runs.go``. All fields besides
    :attr:`type` are optional — the runtime only fills the ones the
    underlying AgentField event populated. ``raw`` always carries the
    original payload so debug UIs can introspect anything the
    structured fields don't surface (trace ids, tool-call detail, …).
    """

    model_config = ConfigDict(extra="allow")

    type: RunEventType
    run_id: str | None = None
    execution_id: str | None = None
    agent: str | None = None
    node_id: str | None = None
    step_id: str | None = None
    reasoner: str | None = None
    status: str | None = None
    started_at: str | None = None
    ended_at: str | None = None
    updated_at: str | None = None
    duration_ms: int | None = None
    cost_usd: float | None = None
    tool_calls: list[Any] | None = None
    tokens: dict[str, Any] | None = None
    error: str | None = None
    reason: str | None = None
    raw: dict[str, Any] = Field(default_factory=dict)


def _resolve_base_url() -> str:
    return os.environ.get("AF_STACK_URL", _http.DEFAULT_BASE_URL).rstrip("/")


def _resolve_api_key() -> str | None:
    key = os.environ.get("AF_STACK_API_KEY")
    return key if key else None


def _to_ws_base(base_url: str) -> str:
    parts = urlsplit(base_url)
    scheme = "wss" if parts.scheme == "https" else "ws"
    path = parts.path.rstrip("/")
    return urlunsplit((scheme, parts.netloc, path, "", ""))


def _build_ws_url(
    *,
    tenant_id: str | None,
    user_id: str | None,
    agent: str | None,
    run_id: str | None,
    execution_id: str | None,
) -> str:
    base = _to_ws_base(_resolve_base_url())
    params: list[tuple[str, str]] = []
    if tenant_id:
        params.append(("tenant_id", tenant_id))
    if user_id:
        params.append(("user_id", user_id))
    if agent:
        params.append(("agent", agent))
    if run_id:
        params.append(("run_id", run_id))
    if execution_id:
        params.append(("execution_id", execution_id))
    api_key = _resolve_api_key()
    if api_key is not None:
        params.append(("api_key", api_key))
    qs = urlencode(params) if params else ""
    return f"{base}{_http.API_PREFIX}/realtime/runs" + (f"?{qs}" if qs else "")


def subscribe(
    *,
    tenant_id: str | None = None,
    user_id: str | None = None,
    agent: str | None = None,
    run_id: str | None = None,
    execution_id: str | None = None,
) -> AsyncIterator[RunEvent]:
    """Subscribe to live AgentField run events over WebSocket.

    Returns an async iterator yielding :class:`RunEvent` objects as
    they arrive. Iteration ends when the runtime closes the socket
    (e.g. the watched execution finished, AgentField went down and the
    runtime degraded gracefully, or the SDK process was cancelled).

    All filter arguments are optional and applied server-side. When
    multi-tenancy is enabled on the runtime, ``tenant_id`` is ignored
    in favour of the caller's resolved tenant — this is a safety
    invariant, not a bug. Pass ``execution_id`` to pin the subscription
    to a single execution; the runtime then uses AgentField's
    per-execution stream rather than the global one.

    Returned as a sync function (despite being async-iterable) so
    callers don't need ``await`` before iterating::

        async for evt in suite.runs.subscribe(run_id="run-123"):
            print(evt.type, evt.status)

    The first ``__anext__`` opens the WebSocket. Argument validation
    happens up-front so input errors raise immediately.

    Raises :class:`RuntimeError` (on first iteration) if the optional
    ``websockets`` package is not installed. Raises
    :class:`af_stack._http.AFStackError` (on first iteration) if the
    runtime rejects the subscription (e.g. ``AGENTFIELD_NOT_CONFIGURED``
    or ``TENANT_REQUIRED``).
    """
    url = _build_ws_url(
        tenant_id=tenant_id,
        user_id=user_id,
        agent=agent,
        run_id=run_id,
        execution_id=execution_id,
    )
    return _stream_run_events(url)


async def _stream_run_events(url: str) -> AsyncIterator[RunEvent]:
    """Open the WebSocket and yield parsed run events until close.

    Split out so :func:`subscribe` can do argument validation up-front
    while the actual generator only runs once the caller iterates.
    """
    # Lazy import so the SDK still imports without `websockets`
    # available. Same pattern as af_stack.realtime.subscribe; we want
    # a clear install message rather than a cryptic ImportError at
    # module load.
    try:
        from websockets.asyncio.client import connect as ws_connect
    except ImportError as exc:  # pragma: no cover — surfaced verbatim
        raise RuntimeError(
            "suite.runs.subscribe requires the `websockets` package. "
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
            # If the runtime hasn't filled ``raw`` itself (older shapes
            # always include it; we tolerate the legacy case), surface
            # the whole payload so callers can still introspect it.
            if "raw" not in payload:
                payload = {**payload, "raw": payload.copy()}
            yield RunEvent.model_validate(payload)


__all__ = [
    "RunEvent",
    "RunEventType",
    "subscribe",
]
