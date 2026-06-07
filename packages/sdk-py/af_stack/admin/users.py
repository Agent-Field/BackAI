# SPDX-License-Identifier: Apache-2.0

"""``suite.admin.users.*`` — user lookup.

The admin user surface in v1 is read-only — provisioning happens via
auth providers (the dashboard's identity flow). Mirrors:

==================================  =========================================
SDK call                            Endpoint
==================================  =========================================
:func:`list`                        ``GET /api/v1/admin/users``
==================================  =========================================
"""

from __future__ import annotations

from typing import Any

from .. import _http
from ._models import UserList


async def list(  # noqa: A001 — mirrors the JS ``users.list()`` ergonomics
    *,
    tenant: str | None = None,
    search: str | None = None,
) -> UserList:
    """List users, optionally restricted to a tenant and/or matching ``search``.

    ``tenant``: only return members of the given tenant id.
    ``search``: case-insensitive substring against ``email`` and ``name``.
    """
    params: dict[str, Any] = {}
    if tenant:
        params["tenant"] = tenant
    if search:
        params["search"] = search
    body = await _http.request_json(
        "GET",
        "/admin/users",
        params=params if params else None,
    )
    return UserList.model_validate(body or {"users": []})


__all__ = ["list"]