# SPDX-License-Identifier: Apache-2.0

"""``suite.approvals.*`` — general human decision gates.

These are AF Stack workflow approvals, not AgentField run approvals.
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from . import _http

ApprovalStatus = Literal["pending", "approved", "denied", "cancelled"]
DecisionStatus = Literal["approved", "denied", "cancelled"]


class Approval(BaseModel):
    """One tenant-scoped approval request."""

    model_config = ConfigDict(extra="allow")

    id: str
    tenant_id: str
    requested_by: str | None = None
    kind: str
    payload: dict[str, Any] = Field(default_factory=dict)
    status: ApprovalStatus
    decided_by: str | None = None
    decided_at: str | None = None
    decision_note: str | None = None
    created_at: str
    updated_at: str


class ApprovalList(BaseModel):
    """Paginated approval list."""

    model_config = ConfigDict(extra="allow")

    approvals: list[Approval] = Field(default_factory=list)
    total: int = 0
    has_more: bool = False


async def request(
    *,
    kind: str,
    payload: dict[str, Any] | None = None,
) -> Approval:
    """Request a human decision for a workflow."""
    if not kind.strip():
        raise ValueError("approval kind must be a non-empty string")
    body = await _http.request_json(
        "POST",
        "/approvals",
        json={"kind": kind, "payload": payload or {}},
    )
    return Approval.model_validate(body or {})


async def get(id: str) -> Approval:
    """Fetch one approval request."""
    if not id.strip():
        raise ValueError("approval id must be a non-empty string")
    body = await _http.request_json("GET", f"/approvals/{id}")
    return Approval.model_validate(body or {})


async def list(  # noqa: A001 — mirrors suite.approvals.list()
    *,
    status: ApprovalStatus | str | None = None,
    kind: str | None = None,
    limit: int = 25,
    offset: int = 0,
) -> ApprovalList:
    """List approval requests with optional status/kind filters."""
    params: dict[str, Any] = {"limit": limit, "offset": offset}
    if status:
        params["status"] = status
    if kind:
        params["kind"] = kind
    body = await _http.request_json("GET", "/approvals", params=params)
    return ApprovalList.model_validate(body or {})


async def decide(
    id: str,
    *,
    status: DecisionStatus,
    decision_note: str | None = None,
) -> Approval:
    """Approve, deny, or cancel a pending request."""
    if not id.strip():
        raise ValueError("approval id must be a non-empty string")
    body = await _http.request_json(
        "POST",
        f"/approvals/{id}/decide",
        json={"status": status, "decision_note": decision_note},
    )
    return Approval.model_validate(body or {})


__all__ = [
    "Approval",
    "ApprovalList",
    "ApprovalStatus",
    "DecisionStatus",
    "decide",
    "get",
    "list",
    "request",
]
