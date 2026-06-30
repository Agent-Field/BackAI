# SPDX-License-Identifier: Apache-2.0

"""``suite.admin.tenants.*`` — tenant CRUD.

Maps to the admin tenant endpoints in ``apps/dashboard/src/lib/api.ts``:

==================================  =========================================
SDK call                            Endpoint
==================================  =========================================
:func:`list`                        ``GET    /api/v1/admin/tenants``
:func:`get`                         ``GET    /api/v1/admin/tenants/{id}``
:func:`create`                      ``POST   /api/v1/admin/tenants``
:func:`update`                      ``PATCH  /api/v1/admin/tenants/{id}``
:func:`delete`                      ``DELETE /api/v1/admin/tenants/{id}``
==================================  =========================================

All endpoints are gated by the runtime's ``multi-tenancy`` module flag —
calls against a disabled runtime surface as
:class:`af_stack._http.AFStackError` with ``code == "MT_DISABLED"``.
"""

from __future__ import annotations

from typing import Any
from urllib.parse import quote

from .. import _http
from ._models import (
    CreateTenantInput,
    Tenant,
    TenantDetail,
    TenantList,
    UpdateTenantInput,
)


async def list() -> TenantList:  # noqa: A001 — mirrors the JS ``tenants.list()`` ergonomics
    """List all non-soft-deleted tenants."""
    body = await _http.request_json("GET", "/admin/tenants")
    return TenantList.model_validate(body or {"tenants": []})


async def get(id: str) -> TenantDetail:  # noqa: A002 — positional ``id`` matches the dashboard API
    """Fetch a tenant's joined detail view (tenant + members + keys + usage)."""
    if not id:
        raise ValueError("tenant id must be a non-empty string")
    body = await _http.request_json("GET", f"/admin/tenants/{quote(id, safe='')}")
    return TenantDetail.model_validate(body or {})


async def create(input: CreateTenantInput | dict[str, Any]) -> Tenant:  # noqa: A002
    """Create a tenant. Accepts a :class:`CreateTenantInput` or a plain dict.

    The server validates the slug charset, length, and uniqueness. A
    duplicate slug raises :class:`AFStackError` with ``code == "CONFLICT"``.
    """
    payload = _as_dict(input, CreateTenantInput)
    body = await _http.request_json("POST", "/admin/tenants", json=payload)
    return Tenant.model_validate(body or {})


async def update(
    id: str,  # noqa: A002 — positional ``id`` matches the dashboard API
    input: UpdateTenantInput | dict[str, Any],
) -> Tenant:
    """Update selected tenant fields.

    Fields omitted from ``input`` are left untouched; passing an empty
    map for ``settings``/``quota`` clears them. The server returns the
    full updated row.
    """
    if not id:
        raise ValueError("tenant id must be a non-empty string")
    payload = _as_dict(input, UpdateTenantInput)
    body = await _http.request_json("PATCH", f"/admin/tenants/{quote(id, safe='')}", json=payload)
    return Tenant.model_validate(body or {})


async def delete(id: str) -> bool:  # noqa: A001, A002
    """Soft-delete a tenant. Returns ``True`` on success.

    Hard delete is intentionally not exposed; the runtime sets
    ``deleted_at = now()`` so audits remain queryable.
    """
    if not id:
        raise ValueError("tenant id must be a non-empty string")
    body = await _http.request_json("DELETE", f"/admin/tenants/{quote(id, safe='')}")
    if not isinstance(body, dict):
        return True
    return bool(body.get("deleted", True))


def _as_dict(
    value: Any,
    model: type[CreateTenantInput | UpdateTenantInput],
) -> dict[str, Any]:
    """Coerce ``value`` (dict OR ``BaseModel``) into a clean wire dict.

    Pydantic's ``model_dump(exclude_none=True)`` drops ``None`` fields so
    we don't accidentally null-out server state on partial updates.
    """
    if isinstance(value, model):
        return value.model_dump(exclude_none=True)
    if isinstance(value, dict):
        return {k: v for k, v in value.items() if v is not None}
    raise TypeError(f"input must be a {model.__name__} or dict, got {type(value).__name__}")


__all__ = ["create", "delete", "get", "list", "update"]
