# SPDX-License-Identifier: Apache-2.0

"""``suite.webhooks.*`` — webhook outbox + delivery feed operations.

Maps to the REST surface defined in ``apps/dashboard/src/lib/api.ts`` under
the Phase 10.2 + 10.3 Webhooks section:

==================================  ====================================
SDK call                            Endpoint
==================================  ====================================
:func:`send`                        ``POST   /api/v1/webhooks/send``
:func:`list`                        ``GET    /api/v1/webhooks/deliveries``
:func:`get`                         ``GET    /api/v1/webhooks/deliveries/{id}``
:func:`retry`                       ``POST   /api/v1/webhooks/deliveries/{id}/retry``
==================================  ====================================

The runtime persists every delivery (request body, response status,
latency, attempts) so callers can audit, replay, and trigger manual
retries. The outbound retry worker drains the queue every 2s with linear
backoff capped at 5 minutes; the dashboard uses :func:`list` to render a
unified inbound + outbound feed (filterable by ``direction``).

The :class:`WebhookDelivery` and :class:`WebhookDeliveryList` Pydantic
models mirror the zod schemas in ``lib/api.ts`` exactly — keep them in
sync when the contract changes.
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from . import _http

WebhookDirection = Literal["inbound", "outbound"]
WebhookStatus = Literal[
    "queued",
    "delivering",
    "succeeded",
    "failed",
    "dropped_duplicate",
    "dropped_invalid_signature",
]

_ALLOWED_DIRECTIONS: tuple[str, ...] = ("inbound", "outbound")


class WebhookDelivery(BaseModel):
    """One webhook delivery row.

    Mirrors ``WebhookDeliverySchema`` in ``apps/dashboard/src/lib/api.ts``.
    Pointer-style nullable fields use ``| None`` so the wire's ``null``
    round-trips faithfully.
    """

    model_config = ConfigDict(extra="allow")

    id: str
    endpoint_id: str | None = None
    direction: WebhookDirection
    destination: str
    event_type: str
    status: WebhookStatus
    attempts: int = 0
    response_status: int | None = None
    response_ms: int | None = None
    body_preview: str | None = None
    body_storage_url: str | None = None
    last_error: str | None = None
    scheduled_at: str
    delivered_at: str | None = None
    created_at: str


class WebhookDeliveryList(BaseModel):
    """Paginated list of webhook deliveries.

    Mirrors ``WebhookDeliveryListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    deliveries: list[WebhookDelivery] = Field(default_factory=list)
    total: int = 0
    has_more: bool = False


async def send(
    url: str,
    event_type: str,
    body: Any,
    *,
    headers: dict[str, str] | None = None,
    immediate: bool = False,
) -> WebhookDelivery:
    """Enqueue an outbound webhook delivery and return the persisted row.

    ``url`` is the destination ``http(s)://`` endpoint; the runtime POSTs
    JSON-encoded ``body`` with ``Content-Type: application/json`` and an
    ``X-AF-Event-Type`` header. Custom ``headers`` are merged on top.

    When ``immediate=True`` the runtime POSTs synchronously on the
    request thread and returns the terminal row (``succeeded`` /
    ``failed``). Otherwise the row is inserted at status ``queued`` and
    the background worker drains it on the next 2-second tick.
    """
    if not url:
        raise ValueError("webhook url must be a non-empty string")
    if not event_type:
        raise ValueError("webhook event_type must be a non-empty string")
    payload: dict[str, Any] = {
        "url": url,
        "event_type": event_type,
        "body": body,
        "immediate": bool(immediate),
    }
    if headers is not None:
        payload["headers"] = dict(headers)
    res = await _http.request_json("POST", "/webhooks/send", json=payload)
    return WebhookDelivery.model_validate(res or {})


async def list(  # noqa: A001 — mirrors the JS `webhooks.list()` ergonomics
    *,
    direction: WebhookDirection | str | None = None,
    endpoint_id: str | None = None,
    tenant: str | None = None,
    status: WebhookStatus | str | None = None,
    limit: int = 50,
    offset: int = 0,
) -> WebhookDeliveryList:
    """List webhook deliveries with optional filters.

    Filter parameter names match the dashboard API. ``direction`` is one
    of ``"inbound"``, ``"outbound"``, or ``None`` (both). The list is
    sorted DESC by ``created_at`` and capped at 200 rows per page.
    """
    if limit <= 0:
        raise ValueError("limit must be a positive integer")
    if offset < 0:
        raise ValueError("offset must be non-negative")
    if direction is not None and direction not in _ALLOWED_DIRECTIONS:
        allowed = ", ".join(_ALLOWED_DIRECTIONS)
        raise ValueError(f"direction must be one of: {allowed}; got {direction!r}")

    params: dict[str, Any] = {"limit": limit, "offset": offset}
    if direction is not None:
        params["direction"] = direction
    if endpoint_id is not None:
        params["endpoint_id"] = endpoint_id
    if tenant is not None:
        params["tenant"] = tenant
    if status is not None:
        params["status"] = status

    body = await _http.request_json("GET", "/webhooks/deliveries", params=params)
    return WebhookDeliveryList.model_validate(
        body or {"deliveries": [], "total": 0, "has_more": False}
    )


async def get(id: str) -> WebhookDelivery:
    """Fetch a single delivery row by id."""
    if not id:
        raise ValueError("webhook delivery id must be a non-empty string")
    body = await _http.request_json("GET", f"/webhooks/deliveries/{id}")
    return WebhookDelivery.model_validate(body or {})


async def retry(id: str) -> WebhookDelivery:
    """Re-queue a delivery for the retry worker.

    Resets ``attempts`` to 0, clears ``last_error``, and bumps the
    delivery back to ``status='queued'`` with ``scheduled_at=now()``.
    Returns the refreshed row.
    """
    if not id:
        raise ValueError("webhook delivery id must be a non-empty string")
    body = await _http.request_json("POST", f"/webhooks/deliveries/{id}/retry")
    return WebhookDelivery.model_validate(body or {})


__all__ = [
    "WebhookDelivery",
    "WebhookDeliveryList",
    "WebhookDirection",
    "WebhookStatus",
    "get",
    "list",
    "retry",
    "send",
]
