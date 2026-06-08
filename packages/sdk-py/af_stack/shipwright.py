# SPDX-License-Identifier: Apache-2.0

"""``suite.shipwright.*`` — autonomous coding-agent task factory.

AF Stack stores only task and patch metadata. AgentField owns the coding-agent
run, harness calls, live logs, spans, traces, and memory.
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from . import _http

ShipwrightStatus = Literal["queued", "running", "succeeded", "failed", "cancelled"]


class ShipwrightTask(BaseModel):
    """One Shipwright task row."""

    model_config = ConfigDict(extra="allow")

    id: str
    tenant_id: str
    user_id: str | None = None
    title: str
    description: str
    repo_url: str
    status: ShipwrightStatus
    run_id: str | None = None
    created_at: str
    updated_at: str


class ShipwrightPatch(BaseModel):
    """Patch pointer produced by a completed Shipwright task."""

    model_config = ConfigDict(extra="allow")

    task_id: str
    ref: str
    summary: str = ""
    diff_url: str | None = None
    created_at: str


class ShipwrightTaskResponse(BaseModel):
    """Task envelope returned by create/get/complete."""

    model_config = ConfigDict(extra="allow")

    task: ShipwrightTask
    patches: list[ShipwrightPatch] = Field(default_factory=list)
    agent_call: str | None = None
    agentfield_url: str | None = None
    details_url: str | None = None


class ShipwrightTaskList(BaseModel):
    """Paginated list of Shipwright tasks."""

    model_config = ConfigDict(extra="allow")

    tasks: list[ShipwrightTask] = Field(default_factory=list)
    total: int = 0
    has_more: bool = False


async def create(
    *,
    title: str,
    description: str,
    repo_url: str,
    harness_provider: str | None = None,
    model: str | None = None,
) -> ShipwrightTaskResponse:
    """Create a Shipwright task and start the AgentField async execution."""
    if not title.strip():
        raise ValueError("title must be a non-empty string")
    if not description.strip():
        raise ValueError("description must be a non-empty string")
    if not repo_url.strip():
        raise ValueError("repo_url must be a non-empty string")
    payload: dict[str, Any] = {
        "title": title,
        "description": description,
        "repo_url": repo_url,
    }
    if harness_provider is not None:
        payload["harness_provider"] = harness_provider
    if model is not None:
        payload["model"] = model
    body = await _http.request_json("POST", "/shipwright/tasks", json=payload)
    return ShipwrightTaskResponse.model_validate(body or {})


async def get(id: str) -> ShipwrightTaskResponse:
    """Fetch one Shipwright task and its patch pointers."""
    if not id:
        raise ValueError("task id must be a non-empty string")
    body = await _http.request_json("GET", f"/shipwright/tasks/{id}")
    return ShipwrightTaskResponse.model_validate(body or {})


async def list(  # noqa: A001 — mirrors JS `shipwright.list()`
    *,
    status: ShipwrightStatus | str | None = None,
    limit: int = 25,
    offset: int = 0,
) -> ShipwrightTaskList:
    """List Shipwright tasks with optional status filter."""
    params: dict[str, Any] = {"limit": limit, "offset": offset}
    if status is not None:
        params["status"] = status
    body = await _http.request_json("GET", "/shipwright/tasks", params=params)
    return ShipwrightTaskList.model_validate(body or {})


async def complete(
    id: str,
    *,
    status: Literal["succeeded", "failed", "cancelled"] | None = None,
    ref: str | None = None,
    summary: str | None = None,
    diff_url: str | None = None,
) -> ShipwrightTaskResponse:
    """Record completion metadata and an optional patch pointer."""
    if not id:
        raise ValueError("task id must be a non-empty string")
    payload: dict[str, Any] = {}
    if status is not None:
        payload["status"] = status
    if ref is not None:
        payload["ref"] = ref
    if summary is not None:
        payload["summary"] = summary
    if diff_url is not None:
        payload["diff_url"] = diff_url
    body = await _http.request_json(
        "POST", f"/shipwright/tasks/{id}/complete", json=payload
    )
    return ShipwrightTaskResponse.model_validate(body or {})


__all__ = [
    "ShipwrightPatch",
    "ShipwrightStatus",
    "ShipwrightTask",
    "ShipwrightTaskList",
    "ShipwrightTaskResponse",
    "complete",
    "create",
    "get",
    "list",
]
