# SPDX-License-Identifier: Apache-2.0

"""``suite.agents.*`` — invoking AgentField from outside an agent.

Maps directly to the REST surface documented in ``TECH-SPEC.md §4.4``:

==================================  ====================================
SDK call                            Endpoint
==================================  ====================================
:func:`call`                        ``POST   /agents/{ns}.{func}``
:func:`call_async`                  ``POST   /agents/async/{ns}.{func}``
:func:`stream`                      ``GET    /agents/stream/{ns}.{func}``
:func:`status`                      ``GET    /executions/{id}``
:func:`cancel`                      ``DELETE /executions/{id}``
:func:`approve`                     ``POST   /executions/{id}/approve``
:func:`deny`                        ``POST   /executions/{id}/deny``
:func:`pending_approvals`           ``GET    /approvals/pending``
==================================  ====================================

This module deliberately does **not** expose a ``llm.chat`` style bypass.
All model calls flow through AgentField via these endpoints.
"""

from __future__ import annotations

from collections.abc import AsyncIterator
from typing import Any

from pydantic import BaseModel, ConfigDict, Field

from . import _http


class CallResult(BaseModel):
    """Synchronous ``agents.call`` response."""

    model_config = ConfigDict(extra="allow")

    execution_id: str = Field(..., description="AF execution id")
    output: Any = Field(default=None, description="Agent return value")
    status: str = Field(default="done")


class AsyncHandle(BaseModel):
    """Reference to an async execution started via ``agents.call_async``."""

    model_config = ConfigDict(extra="allow")

    execution_id: str
    status: str = "queued"
    webhook_url: str | None = None

    # Allow ``handle.id`` for ergonomic parity with TS clients.
    @property
    def id(self) -> str:
        return self.execution_id


class ExecutionStatus(BaseModel):
    """``agents.status`` response."""

    model_config = ConfigDict(extra="allow")

    execution_id: str
    status: str
    started_at: str | None = None
    ended_at: str | None = None
    output: Any = None
    error: dict[str, Any] | None = None


class PendingApproval(BaseModel):
    """One row from ``agents.pending_approvals``."""

    model_config = ConfigDict(extra="allow")

    execution_id: str
    agent: str
    requested_at: str | None = None
    reason: str | None = None
    metadata: dict[str, Any] = Field(default_factory=dict)


def _agent_path(name: str) -> str:
    """Validate ``ns.func`` shape and return the URL path segment."""
    if not name or "." not in name:
        raise ValueError(f"agent name must be of the form 'namespace.function', got {name!r}")
    ns, _, func = name.partition(".")
    if not ns or not func:
        raise ValueError(f"agent name must be of the form 'namespace.function', got {name!r}")
    return name


async def call(
    name: str,
    input: dict[str, Any] | None = None,
    *,
    timeout_s: float | None = None,
    metadata: dict[str, Any] | None = None,
) -> CallResult:
    """Synchronously call an AgentField agent and return its result.

    ``name`` must be of the form ``"namespace.function"``.
    """
    path = f"/agents/{_agent_path(name)}"
    payload: dict[str, Any] = {"input": input or {}}
    if metadata is not None:
        payload["metadata"] = metadata
    if timeout_s is not None:
        payload["timeout_s"] = timeout_s
    body = await _http.request_json(
        "POST",
        path,
        json=payload,
        timeout=timeout_s,
    )
    return CallResult.model_validate(body or {})


async def call_async(
    name: str,
    input: dict[str, Any] | None = None,
    *,
    webhook_url: str | None = None,
    metadata: dict[str, Any] | None = None,
) -> AsyncHandle:
    """Enqueue an agent execution and return immediately with a handle."""
    path = f"/agents/async/{_agent_path(name)}"
    payload: dict[str, Any] = {"input": input or {}}
    if webhook_url is not None:
        payload["webhook_url"] = webhook_url
    if metadata is not None:
        payload["metadata"] = metadata
    body = await _http.request_json("POST", path, json=payload)
    return AsyncHandle.model_validate(body or {})


async def stream(
    name: str,
    input: dict[str, Any] | None = None,
) -> AsyncIterator[dict[str, Any]]:
    """Stream events for an agent invocation via Server-Sent Events.

    Yields dicts of the shape ``{"event": str, "data": Any}``.
    """
    # TODO: end-to-end SSE test coverage. Currently exercised only via
    # unit tests on ``_http.stream_sse`` parser behaviour.
    path = f"/agents/stream/{_agent_path(name)}"
    payload: dict[str, Any] = {"input": input or {}}
    async for event in _http.stream_sse("POST", path, json=payload):
        yield event


async def status(execution_id: str) -> ExecutionStatus:
    """Fetch the current status of an execution."""
    body = await _http.request_json("GET", f"/executions/{execution_id}")
    return ExecutionStatus.model_validate(body or {})


async def cancel(execution_id: str, *, reason: str | None = None) -> None:
    """Cancel a running execution."""
    params = {"reason": reason} if reason else None
    await _http.request_json("DELETE", f"/executions/{execution_id}", params=params)


async def approve(
    execution_id: str,
    *,
    by_user_id: str | None = None,
    note: str | None = None,
) -> None:
    """Approve a paused HITL execution."""
    payload: dict[str, Any] = {}
    if by_user_id is not None:
        payload["by_user_id"] = by_user_id
    if note is not None:
        payload["note"] = note
    await _http.request_json("POST", f"/executions/{execution_id}/approve", json=payload)


async def deny(
    execution_id: str,
    *,
    reason: str | None = None,
    by_user_id: str | None = None,
) -> None:
    """Deny a paused HITL execution."""
    payload: dict[str, Any] = {}
    if reason is not None:
        payload["reason"] = reason
    if by_user_id is not None:
        payload["by_user_id"] = by_user_id
    await _http.request_json("POST", f"/executions/{execution_id}/deny", json=payload)


async def pending_approvals() -> list[PendingApproval]:
    """List executions currently waiting on a HITL approval."""
    body = await _http.request_json("GET", "/approvals/pending")
    rows = body if isinstance(body, list) else (body or {}).get("items") or []
    return [PendingApproval.model_validate(row) for row in rows]


__all__ = [
    "AsyncHandle",
    "CallResult",
    "ExecutionStatus",
    "PendingApproval",
    "approve",
    "call",
    "call_async",
    "cancel",
    "deny",
    "pending_approvals",
    "status",
    "stream",
]
