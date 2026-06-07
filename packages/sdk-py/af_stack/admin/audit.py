# SPDX-License-Identifier: Apache-2.0

"""``suite.admin.audit.*`` — audit log search.

Maps to:

==================================  =========================================
SDK call                            Endpoint
==================================  =========================================
:func:`list`                        ``GET /api/v1/admin/audit``
==================================  =========================================

The endpoint accepts free-form filters via query params; results are
ordered ``occurred_at desc`` and paginated.
"""

from __future__ import annotations

from datetime import datetime
from typing import Any

from .. import _http
from ._models import AuditList


async def list(  # noqa: A001 — mirrors the JS ``audit.list()`` ergonomics
    *,
    tenant: str | None = None,
    action: str | None = None,
    from_: str | datetime | None = None,
    to: str | datetime | None = None,
    limit: int = 100,
    offset: int = 0,
) -> AuditList:
    """List audit log entries with optional filters.

    ``from_`` and ``to`` are RFC3339 timestamps (strings) or
    :class:`datetime` objects. ``from_`` uses a trailing underscore
    because ``from`` is a Python keyword; the wire param is plain
    ``from``.
    """
    if limit <= 0:
        raise ValueError("limit must be a positive integer")
    if offset < 0:
        raise ValueError("offset must be non-negative")
    params: dict[str, Any] = {"limit": limit, "offset": offset}
    if tenant:
        params["tenant"] = tenant
    if action:
        params["action"] = action
    if from_ is not None:
        params["from"] = _to_iso(from_)
    if to is not None:
        params["to"] = _to_iso(to)
    body = await _http.request_json("GET", "/admin/audit", params=params)
    return AuditList.model_validate(body or {"entries": [], "total": 0, "has_more": False})


def _to_iso(value: str | datetime) -> str:
    """Serialise a :class:`datetime` to RFC3339, leaving strings alone.

    Naive datetimes are treated as UTC for predictability — same
    convention used by :mod:`af_stack.jobs`.
    """
    if isinstance(value, datetime):
        iso = value.isoformat()
        if value.tzinfo is None:
            return iso + "Z"
        return iso
    return value


__all__ = ["list"]