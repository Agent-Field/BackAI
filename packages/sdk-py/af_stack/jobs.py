"""``suite.jobs.*`` — background job operations.

Maps to the REST surface defined in ``apps/dashboard/src/lib/api.ts`` under
the Phase 5 Jobs section:

==================================  ====================================
SDK call                            Endpoint
==================================  ====================================
:func:`enqueue`                     ``POST   /api/v1/jobs``
:func:`get`                         ``GET    /api/v1/jobs/{id}``
:func:`retry`                       ``POST   /api/v1/jobs/{id}/retry``
:func:`list`                        ``GET    /api/v1/jobs``
==================================  ====================================

The :class:`Job` and :class:`JobList` Pydantic models mirror the zod schemas
in ``lib/api.ts`` exactly — keep them in sync when the contract changes.

Admin operations (job *definitions*, cron management, etc.) live in the
post-v1 admin SDK. They're intentionally absent here.
"""

from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from . import _http

JobState = Literal[
    "available",
    "running",
    "completed",
    "discarded",
    "cancelled",
    "retryable",
    "scheduled",
    "pending",
]


class JobError(BaseModel):
    """One row from a job's ``errors`` array."""

    model_config = ConfigDict(extra="allow")

    at: str
    attempt: int
    error: str


class Job(BaseModel):
    """A background job row.

    Mirrors ``JobSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    id: str
    name: str
    args: Any = None
    state: JobState
    tenant_id: str | None = None
    attempt: int
    max_attempts: int
    scheduled_at: str
    enqueued_at: str
    attempted_at: str | None = None
    finalized_at: str | None = None
    errors: list[JobError] | None = None


class JobList(BaseModel):
    """Paginated list of jobs.

    Mirrors ``JobListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    jobs: list[Job] = Field(default_factory=list)
    total: int = 0
    has_more: bool = False


async def enqueue(
    name: str,
    args: dict[str, Any] | None = None,
    *,
    scheduled_at: datetime | None = None,
    max_attempts: int | None = None,
) -> Job:
    """Enqueue a background job and return the persisted row.

    ``name`` is the job definition name (e.g. ``"send-welcome-email"``).
    ``args`` is the JSON-serialisable argument payload forwarded verbatim
    to the worker. ``scheduled_at`` defers execution until the given time;
    when omitted the runtime queues immediately.
    """
    if not name:
        raise ValueError("job name must be a non-empty string")
    payload: dict[str, Any] = {
        "name": name,
        "args": args if args is not None else {},
    }
    if scheduled_at is not None:
        payload["scheduled_at"] = _serialize_dt(scheduled_at)
    if max_attempts is not None:
        if max_attempts < 1 or max_attempts > 50:
            raise ValueError("max_attempts must be between 1 and 50")
        payload["max_attempts"] = max_attempts
    body = await _http.request_json("POST", "/jobs", json=payload)
    return Job.model_validate(body or {})


async def get(id: str) -> Job:
    """Fetch a job by id."""
    if not id:
        raise ValueError("job id must be a non-empty string")
    body = await _http.request_json("GET", f"/jobs/{id}")
    return Job.model_validate(body or {})


async def retry(id: str) -> Job:
    """Force-retry a job (resets state to ``available``)."""
    if not id:
        raise ValueError("job id must be a non-empty string")
    body = await _http.request_json("POST", f"/jobs/{id}/retry")
    return Job.model_validate(body or {})


async def list(  # noqa: A001 — mirrors the JS `jobs.list()` ergonomics
    *,
    name: str | None = None,
    state: JobState | str | None = None,
    tenant: str | None = None,
    limit: int = 50,
    offset: int = 0,
) -> JobList:
    """List jobs with optional filters.

    Filter parameter names match the dashboard API (``name``, ``state``,
    ``tenant``).
    """
    params: dict[str, Any] = {"limit": limit, "offset": offset}
    if name is not None:
        params["name"] = name
    if state is not None:
        params["state"] = state
    if tenant is not None:
        params["tenant"] = tenant
    body = await _http.request_json("GET", "/jobs", params=params)
    return JobList.model_validate(body or {})


def _serialize_dt(dt: datetime) -> str:
    """ISO-8601 with a ``Z`` suffix for naive UTC datetimes."""
    iso = dt.isoformat()
    # If the datetime is naive, treat as UTC for predictability.
    if dt.tzinfo is None:
        return iso + "Z"
    return iso


__all__ = [
    "Job",
    "JobError",
    "JobList",
    "JobState",
    "enqueue",
    "get",
    "list",
    "retry",
]
