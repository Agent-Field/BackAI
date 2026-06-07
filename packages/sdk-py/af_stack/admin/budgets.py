# SPDX-License-Identifier: Apache-2.0

"""``suite.admin.budgets.*`` — per-tenant LLM spend budgets.

Maps to the admin budgets endpoints in
``apps/dashboard/src/lib/api.ts`` (Phase 7):

==================================  =========================================
SDK call                            Endpoint
==================================  =========================================
:func:`list`                        ``GET    /api/v1/admin/budgets``
:func:`get`                         ``GET    /api/v1/admin/budgets/{id}``
:func:`set`                         ``PUT    /api/v1/admin/budgets``
==================================  =========================================

Budgets gate LLM calls — when a tenant's monthly spend exceeds
:attr:`Budget.monthly_usd`, subsequent gateway requests fail with HTTP
402 and ``code == "BUDGET_EXCEEDED"``. Setting a budget is idempotent;
re-PUTting the same tenant_id updates the row.
"""

from __future__ import annotations

from typing import Any
from urllib.parse import quote

from .. import _http
from ._models import Budget, BudgetList, SetBudgetInput


async def list() -> BudgetList:  # noqa: A001 — mirrors ``budgets.list()``
    """List every tenant's budget + current-period spend."""
    body = await _http.request_json("GET", "/admin/budgets")
    return BudgetList.model_validate(body or {"budgets": []})


async def get(tenant_id: str) -> Budget:
    """Fetch a single tenant's budget by id."""
    if not tenant_id:
        raise ValueError("tenant_id must be a non-empty string")
    body = await _http.request_json(
        "GET", f"/admin/budgets/{quote(tenant_id, safe='')}"
    )
    return Budget.model_validate(body or {})


async def set(input: SetBudgetInput | dict[str, Any]) -> Budget:  # noqa: A001
    """Create or update a tenant's monthly budget.

    Accepts a :class:`SetBudgetInput` or a plain dict with
    ``tenant_id``, ``monthly_usd``, and (optional) ``alert_threshold_pct``.
    Returns the persisted :class:`Budget` row.
    """
    payload = _as_dict(input)
    if not payload.get("tenant_id"):
        raise ValueError("input.tenant_id must be a non-empty string")
    if not isinstance(payload.get("monthly_usd"), (int, float)):
        raise ValueError("input.monthly_usd must be a number")
    if payload["monthly_usd"] <= 0:
        raise ValueError("input.monthly_usd must be positive")
    body = await _http.request_json("PUT", "/admin/budgets", json=payload)
    return Budget.model_validate(body or {})


def _as_dict(value: Any) -> dict[str, Any]:
    """Coerce a :class:`SetBudgetInput` or dict into a wire payload."""
    if isinstance(value, SetBudgetInput):
        return value.model_dump(exclude_none=True)
    if isinstance(value, dict):
        return {k: v for k, v in value.items() if v is not None}
    raise TypeError(
        f"input must be SetBudgetInput or dict, got {type(value).__name__}"
    )


__all__ = ["get", "list", "set"]