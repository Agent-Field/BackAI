# SPDX-License-Identifier: Apache-2.0

"""``suite.billing.*`` — Stripe billing + per-tenant usage meters.

Maps to the REST surface defined in ``apps/dashboard/src/lib/api.ts``
under the Phase 10.4 "Billing" section:

==================================  =======================================================
SDK call                            Endpoint
==================================  =======================================================
:func:`customers`                   ``GET    /api/v1/billing/customers``
:func:`customer`                    ``GET    /api/v1/billing/customers/{tenantId}``
:func:`meters`                      ``GET    /api/v1/billing/meters``
:func:`portal_link`                 ``POST   /api/v1/billing/customers/{tenantId}/portal``
:func:`meter`                       ``POST   /api/v1/billing/meter``
:func:`has_budget`                  (in-process — checks the tenant's plan cap)
==================================  =======================================================

The Pydantic models mirror the zod schemas in ``lib/api.ts`` exactly —
keep them in sync when the contract changes.

``has_budget`` has no dedicated endpoint: it's composed client-side from the
customer + usage-meter read endpoints against the configured plan cap.
"""

from __future__ import annotations

from typing import Any, Literal
from urllib.parse import quote

from pydantic import BaseModel, ConfigDict, Field

from . import _http

Bucket = Literal["month", "day"]


class BillingCustomer(BaseModel):
    """One billing customer row.

    Mirrors ``BillingCustomerSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    tenant_id: str
    stripe_customer_id: str | None = None
    email: str | None = None
    plan: str = "free"
    trial_ends_at: str | None = None
    current_period_end: str | None = None
    subscription_status: str | None = None
    created_at: str
    updated_at: str


class BillingCustomerList(BaseModel):
    """Paginated list of billing customers.

    Mirrors ``BillingCustomerListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    customers: list[BillingCustomer] = Field(default_factory=list)


class UsageMeter(BaseModel):
    """One usage meter bucket.

    Mirrors ``UsageMeterSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    meter: str
    tenant_id: str
    period_start: str
    period_end: str
    quantity: float
    cost_usd: float | None = None
    stripe_meter_id: str | None = None
    last_synced_at: str | None = None


class UsageMeterList(BaseModel):
    """List of usage meters with the period total.

    Mirrors ``UsageMeterListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    meters: list[UsageMeter] = Field(default_factory=list)
    total_cost_usd: float = 0.0


class PortalLink(BaseModel):
    """A short-lived Stripe Customer Portal URL.

    Mirrors ``PortalLinkSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    url: str
    expires_at: str


# ─── Read surface ─────────────────────────────────────────────────────────


async def customers() -> BillingCustomerList:
    """List every billing customer."""
    body = await _http.request_json("GET", "/billing/customers")
    return BillingCustomerList.model_validate(body or {"customers": []})


async def customer(tenant_id: str) -> BillingCustomer:
    """Fetch a tenant's billing customer. Synthesised "free" row when
    no billing customer exists yet — the runtime handles the fallback
    so callers always get a populated row."""
    if not tenant_id:
        raise ValueError("tenant_id must be a non-empty string")
    body = await _http.request_json("GET", f"/billing/customers/{quote(tenant_id, safe='')}")
    return BillingCustomer.model_validate(body or {})


async def meters(
    *,
    tenant: str | None = None,
    period_start: str | None = None,
    bucket: Bucket = "month",
) -> UsageMeterList:
    """List usage meters for the given filters.

    Defaults to the current month bucket. Pass ``bucket="day"`` for a
    daily slice (the runtime aligns to midnight UTC).
    """
    params: dict[str, Any] = {}
    if tenant:
        params["tenant"] = tenant
    if period_start:
        params["period_start"] = period_start
    if bucket:
        params["bucket"] = bucket
    body = await _http.request_json("GET", "/billing/meters", params=params)
    return UsageMeterList.model_validate(body or {"meters": [], "total_cost_usd": 0.0})


# ─── Write surface ────────────────────────────────────────────────────────


async def portal_link(tenant_id: str, return_url: str | None = None) -> PortalLink:
    """Mint a short-lived Stripe Customer Portal link for the tenant.

    In stub mode (no ``STRIPE_SECRET_KEY`` on the runtime), the URL points
    at ``example.com`` so the dashboard can still render the button.
    """
    if not tenant_id:
        raise ValueError("tenant_id must be a non-empty string")
    payload: dict[str, Any] = {}
    if return_url:
        payload["return_url"] = return_url
    body = await _http.request_json(
        "POST",
        f"/billing/customers/{quote(tenant_id, safe='')}/portal",
        json=payload or None,
    )
    return PortalLink.model_validate(body or {})


# ─── In-process verbs (forwarded via the customer endpoint) ───────────────


async def meter(
    name: str,
    qty: float,
    *,
    tenant_id: str | None = None,
) -> None:
    """Record ``qty`` units of ``name`` for the tenant's current period.

    The runtime owns the cost computation — the SDK forwards the increment
    to ``POST /api/v1/billing/meter``. Pass ``tenant_id`` to attribute usage
    to a specific tenant; when omitted the runtime meters under its default
    tenant (the normal single-tenant path).
    """
    if not name:
        raise ValueError("meter name must be a non-empty string")
    if qty < 0:
        raise ValueError("qty must be non-negative")
    payload: dict[str, object] = {"name": name, "qty": qty}
    if tenant_id:
        payload["tenant_id"] = tenant_id
    await _http.request_json("POST", "/billing/meter", json=payload)


async def has_budget(tenant_id: str, additional_usd: float) -> bool:
    """Check whether the tenant has room for an additional spend.

    Phase 10.4 implements this on the runtime via
    ``billing.Service.HasBudget``; the SDK fetches the customer + the
    current period's usage and compares against the configured plan
    cap. Returns True permissively when the runtime doesn't have the
    customer/meters data wired (boot mode).
    """
    if not tenant_id:
        return True
    if additional_usd < 0:
        raise ValueError("additional_usd must be non-negative")
    # Compose the gate from the existing read endpoints. Future
    # Phase 10.5 work may expose a dedicated /api/v1/billing/has-budget
    # endpoint that short-circuits this round-trip.
    try:
        cust = await customer(tenant_id)
    except _http.AFStackError:
        return True
    plan_caps = {"free": 10.0, "pro": 500.0, "enterprise": 0.0}
    cap = plan_caps.get(cust.plan, 0.0)
    if cap <= 0:
        # Unknown plan / enterprise == unlimited.
        return True
    usage = await meters(tenant=tenant_id)
    return (usage.total_cost_usd + additional_usd) <= cap


__all__ = [
    "BillingCustomer",
    "BillingCustomerList",
    "Bucket",
    "PortalLink",
    "UsageMeter",
    "UsageMeterList",
    "customer",
    "customers",
    "has_budget",
    "meter",
    "meters",
    "portal_link",
]
