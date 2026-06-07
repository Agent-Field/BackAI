"""``suite.admin.memberships.*`` — tenant<->user role mapping.

Maps to:

==================================  =========================================
SDK call                            Endpoint
==================================  =========================================
:func:`list`                        ``GET    /api/v1/admin/memberships``
:func:`add`                         ``POST   /api/v1/admin/memberships``
:func:`remove`                      ``DELETE /api/v1/admin/memberships/{tenantId}/{userId}``
==================================  =========================================

``add`` upserts the membership (existing (tenant, user) rows are updated
to the new role rather than rejected).
"""

from __future__ import annotations

from typing import Any
from urllib.parse import quote

from .. import _http
from ._models import Membership, MembershipList, Role


async def list(  # noqa: A001 — mirrors the JS ``memberships.list()`` ergonomics
    *,
    tenant: str | None = None,
    user: str | None = None,
) -> MembershipList:
    """List memberships, optionally filtered by tenant id and/or user id."""
    params: dict[str, Any] = {}
    if tenant:
        params["tenant"] = tenant
    if user:
        params["user"] = user
    body = await _http.request_json(
        "GET",
        "/admin/memberships",
        params=params if params else None,
    )
    return MembershipList.model_validate(body or {"memberships": []})


async def add(tenant_id: str, user_id: str, role: Role | str) -> Membership:
    """Add (or upsert the role of) a tenant member.

    The server validates that ``role`` is one of
    ``owner|admin|member|viewer``. Foreign-key violations on either id
    surface as :class:`AFStackError` with ``code == "NOT_FOUND"``.
    """
    if not tenant_id:
        raise ValueError("tenant_id must be a non-empty string")
    if not user_id:
        raise ValueError("user_id must be a non-empty string")
    if not role:
        raise ValueError("role must be a non-empty string")
    body = await _http.request_json(
        "POST",
        "/admin/memberships",
        json={"tenant_id": tenant_id, "user_id": user_id, "role": role},
    )
    return Membership.model_validate(body or {})


async def remove(tenant_id: str, user_id: str) -> bool:
    """Remove a membership. Returns ``True`` on success."""
    if not tenant_id:
        raise ValueError("tenant_id must be a non-empty string")
    if not user_id:
        raise ValueError("user_id must be a non-empty string")
    path = (
        f"/admin/memberships/{quote(tenant_id, safe='')}/"
        f"{quote(user_id, safe='')}"
    )
    body = await _http.request_json("DELETE", path)
    if not isinstance(body, dict):
        return True
    return bool(body.get("deleted", True))


__all__ = ["add", "list", "remove"]
