# SPDX-License-Identifier: Apache-2.0

"""``suite.crons.*`` — cron schedules over the background jobs queue.

Maps to the REST surface in ``services/runtime/internal/server/crons.go``:

==================================  ===========================================
SDK call                            Endpoint
==================================  ===========================================
:func:`list`                        ``GET    /api/v1/crons``
:func:`create`                      ``POST   /api/v1/crons``
:func:`get`                         ``GET    /api/v1/crons/{id}``
:func:`set_active`                  ``PUT    /api/v1/crons/{id}/active``
:func:`delete`                      ``DELETE /api/v1/crons/{id}``
==================================  ===========================================

A cron schedule fires a named job (from ``suite.jobs``) on a recurring
schedule. The ``schedule`` field is a standard cron expression
(``* * * * *``) and ``args`` is the JSON payload passed verbatim to the
job handler.

The runtime returns 503 ``CRONS_NOT_CONFIGURED`` if the scheduler is not
wired (typically because no database is configured). All errors come
through as :class:`af_stack._http.AFStackError`.

.. note::
   The REST surface does **not** expose ``update`` or ``trigger`` verbs at
   this time — only ``set_active`` (toggle the ``is_active`` flag) and
   ``delete``. To change a schedule's expression or args, delete and
   recreate. To run a job once on demand, use ``suite.jobs.enqueue``.
"""

from __future__ import annotations

from typing import Any
from urllib.parse import quote

from pydantic import BaseModel, ConfigDict, Field

from . import _http


class Cron(BaseModel):
    """One cron schedule row.

    Mirrors ``CronSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    id: str
    tenant_id: str | None = None
    name: str
    job_name: str
    schedule: str
    args: dict[str, Any] = Field(default_factory=dict)
    is_active: bool = True
    last_run_at: str | None = None
    next_run_at: str = ""
    created_at: str = ""


class CronList(BaseModel):
    """Paginated list of cron schedules.

    Mirrors ``CronListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    crons: list[Cron] = Field(default_factory=list)


async def list(  # noqa: A001 — mirrors the JS `crons.list()` ergonomics
    *,
    tenant: str | None = None,
) -> CronList:
    """List cron schedules visible to the caller.

    ``tenant`` optionally filters to a single tenant id; omit to let the
    server-side RLS policy scope the result.
    """
    params: dict[str, Any] = {}
    if tenant is not None:
        params["tenant"] = tenant
    body = await _http.request_json("GET", "/crons", params=params or None)
    return CronList.model_validate(body or {"crons": []})


async def create(
    name: str,
    job_name: str,
    schedule: str,
    *,
    args: dict[str, Any] | None = None,
    is_active: bool | None = None,
) -> Cron:
    """Create a cron schedule.

    ``schedule`` is a 5- or 6-field cron expression interpreted by the
    runtime's scheduler. ``args`` is the JSON payload threaded into each
    enqueued job. ``is_active`` defaults server-side to ``True`` when
    omitted — pass ``False`` to register the schedule in a paused state.
    """
    if not name:
        raise ValueError("cron name must be a non-empty string")
    if not job_name:
        raise ValueError("cron job_name must be a non-empty string")
    if not schedule:
        raise ValueError("cron schedule must be a non-empty string")
    payload: dict[str, Any] = {
        "name": name,
        "job_name": job_name,
        "schedule": schedule,
    }
    if args is not None:
        payload["args"] = dict(args)
    if is_active is not None:
        payload["is_active"] = bool(is_active)
    body = await _http.request_json("POST", "/crons", json=payload)
    return Cron.model_validate(body or {})


async def get(id: str) -> Cron:  # noqa: A002 — id matches the REST path segment
    """Fetch a single cron schedule by id."""
    if not id:
        raise ValueError("cron id must be a non-empty string")
    body = await _http.request_json("GET", f"/crons/{quote(id, safe='')}")
    return Cron.model_validate(body or {})


async def set_active(id: str, is_active: bool) -> Cron:  # noqa: A002
    """Toggle a schedule's ``is_active`` flag without deleting it.

    Disabling preserves the schedule row so it can be re-enabled later
    without losing history.
    """
    if not id:
        raise ValueError("cron id must be a non-empty string")
    body = await _http.request_json(
        "PUT",
        f"/crons/{quote(id, safe='')}/active",
        json={"is_active": bool(is_active)},
    )
    return Cron.model_validate(body or {})


async def delete(id: str) -> bool:  # noqa: A002
    """Delete a cron schedule by id. Returns ``True`` on success."""
    if not id:
        raise ValueError("cron id must be a non-empty string")
    body = await _http.request_json("DELETE", f"/crons/{quote(id, safe='')}")
    if not isinstance(body, dict):
        return True
    return bool(body.get("deleted", True))


__all__ = [
    "Cron",
    "CronList",
    "create",
    "delete",
    "get",
    "list",
    "set_active",
]
