# SPDX-License-Identifier: Apache-2.0

"""``suite.notifications.*`` — outbox-style notifications.

Maps to the REST surface defined in ``apps/dashboard/src/lib/api.ts``
under the Phase 10.1 Notifications section:

==================================  ====================================
SDK call                            Endpoint
==================================  ====================================
:func:`send`                        ``POST  /api/v1/notifications``
:func:`email`                       ``POST  /api/v1/notifications`` (kind=email)
:func:`list`                        ``GET   /api/v1/notifications``
:func:`get`                         ``GET   /api/v1/notifications/{id}``
:func:`stats`                       ``GET   /api/v1/notifications/stats``
==================================  ====================================

The runtime persists every send in ``suite_notifications`` at
``status='queued'`` and dispatches via a background worker through the
configured adapter (log-stub for dev, Resend for prod). The dashboard's
"Notifications" tab reads the same outbox so operators see what was
sent, queued, or failed without dipping into provider dashboards.

The :class:`Notification`, :class:`NotificationList`, and
:class:`NotificationStats` Pydantic models mirror the zod schemas in
``lib/api.ts`` exactly — keep them in sync when the contract changes.
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from . import _http

NotificationKind = Literal["email", "sms", "push", "log"]
NotificationStatus = Literal["queued", "sending", "sent", "failed", "skipped"]

_ALLOWED_KINDS: tuple[str, ...] = ("email", "sms", "push", "log")


class Notification(BaseModel):
    """One notification row from the outbox.

    Mirrors ``NotificationSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    id: str
    tenant_id: str | None = None
    kind: NotificationKind
    adapter: str | None = None
    template: str
    to: str
    from_: str | None = Field(default=None, alias="from")
    subject: str | None = None
    data: dict[str, Any] = Field(default_factory=dict)
    status: NotificationStatus
    provider_message_id: str | None = None
    attempts: int = 0
    last_error: str | None = None
    scheduled_at: str
    sent_at: str | None = None
    created_at: str


class NotificationList(BaseModel):
    """Paginated list of notifications.

    Mirrors ``NotificationListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    notifications: list[Notification] = Field(default_factory=list)
    total: int = 0
    has_more: bool = False


class AdapterCount(BaseModel):
    """One entry in :class:`NotificationStats.by_adapter`."""

    model_config = ConfigDict(extra="allow")

    adapter: str
    count: int


class NotificationStats(BaseModel):
    """KPI aggregates for the notifications dashboard.

    Mirrors ``NotificationStatsSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    by_status: dict[str, int] = Field(default_factory=dict)
    by_adapter: list[AdapterCount] = Field(default_factory=list)
    sent_today: int = 0
    failed_today: int = 0


# ─── Operations ───────────────────────────────────────────────────────────


async def send(
    *,
    to: str,
    template: str = "default",
    kind: NotificationKind | str = "email",
    subject: str | None = None,
    from_: str | None = None,
    data: dict[str, Any] | None = None,
    scheduled_at: str | None = None,
) -> Notification:
    """Queue a notification for delivery.

    The row is persisted at ``status='queued'`` and the background worker
    dispatches it through the configured adapter. The returned
    :class:`Notification` carries the queued row — poll :func:`get` or
    watch the dashboard for the terminal transition.
    """
    if not isinstance(to, str) or not to:
        raise ValueError("`to` must be a non-empty string")
    if not isinstance(template, str) or not template:
        raise ValueError("`template` must be a non-empty string")
    if kind not in _ALLOWED_KINDS:
        allowed = ", ".join(_ALLOWED_KINDS)
        raise ValueError(f"`kind` must be one of: {allowed}; got {kind!r}")

    payload: dict[str, Any] = {
        "kind": kind,
        "template": template,
        "to": to,
    }
    if subject is not None:
        payload["subject"] = subject
    if from_ is not None:
        payload["from"] = from_
    if data is not None:
        payload["data"] = dict(data)
    if scheduled_at is not None:
        payload["scheduled_at"] = scheduled_at

    body = await _http.request_json("POST", "/notifications", json=payload)
    return Notification.model_validate(body or {})


async def email(
    to: str,
    subject: str,
    *,
    body: str | None = None,
    template: str = "default",
    data: dict[str, Any] | None = None,
    from_: str | None = None,
    scheduled_at: str | None = None,
) -> Notification:
    """Convenience for ``send(kind="email")``.

    ``body`` is folded into ``data["body"]`` so the default template
    renderer (which substitutes ``{{key}}`` tokens against ``data``)
    can produce a plain-text email without a real template registry.
    """
    if not isinstance(subject, str) or not subject:
        raise ValueError("`subject` must be a non-empty string")
    payload_data: dict[str, Any] = dict(data) if data else {}
    if body is not None:
        payload_data["body"] = body
    return await send(
        kind="email",
        to=to,
        subject=subject,
        template=template,
        data=payload_data,
        from_=from_,
        scheduled_at=scheduled_at,
    )


async def list(  # noqa: A001 — mirrors the JS `notifications.list()` ergonomics
    *,
    tenant: str | None = None,
    status: NotificationStatus | str | None = None,
    kind: NotificationKind | str | None = None,
    limit: int = 50,
    offset: int = 0,
) -> NotificationList:
    """List notifications with optional filters."""
    if limit <= 0:
        raise ValueError("limit must be a positive integer")
    if offset < 0:
        raise ValueError("offset must be non-negative")
    params: dict[str, Any] = {"limit": limit, "offset": offset}
    if tenant is not None:
        params["tenant"] = tenant
    if status is not None:
        params["status"] = status
    if kind is not None:
        params["kind"] = kind
    body = await _http.request_json("GET", "/notifications", params=params)
    return NotificationList.model_validate(
        body or {"notifications": [], "total": 0, "has_more": False}
    )


async def get(id: str) -> Notification:
    """Fetch a notification by id."""
    if not id:
        raise ValueError("notification id must be a non-empty string")
    body = await _http.request_json("GET", f"/notifications/{id}")
    return Notification.model_validate(body or {})


async def stats() -> NotificationStats:
    """Return KPI aggregates for the dashboard's notifications tab."""
    body = await _http.request_json("GET", "/notifications/stats")
    return NotificationStats.model_validate(
        body or {"by_status": {}, "by_adapter": [], "sent_today": 0, "failed_today": 0}
    )


__all__ = [
    "AdapterCount",
    "Notification",
    "NotificationKind",
    "NotificationList",
    "NotificationStats",
    "NotificationStatus",
    "email",
    "get",
    "list",
    "send",
    "stats",
]