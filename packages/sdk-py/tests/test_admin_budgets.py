# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for ``af_stack.admin.budgets``.

Endpoint paths and JSON shapes mirror the canonical contract in
``apps/dashboard/src/lib/api.ts`` (Phase 7 LLM Gateway section).
"""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close
from af_stack.admin import budgets
from af_stack.admin._models import SetBudgetInput

BASE = "http://localhost:8080/api/v1"


def _budget_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "tenant_id": "t-acme",
        "monthly_usd": 100.0,
        "alert_threshold_pct": 80.0,
        "spent_this_period_usd": 12.5,
        "remaining_usd": 87.5,
        "resets_at": "2026-07-01T00:00:00Z",
    }
    base.update(overrides)
    return base


@pytest.fixture(autouse=True)
async def reset_http_client(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "test-key")
    await http_close()
    yield
    await http_close()


async def test_list_returns_budgets() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/admin/budgets").mock(
            return_value=httpx.Response(
                200,
                json={"budgets": [_budget_row(), _budget_row(tenant_id="t-globex")]},
            )
        )
        result = await budgets.list()
    assert [b.tenant_id for b in result.budgets] == ["t-acme", "t-globex"]
    assert result.budgets[0].monthly_usd == 100.0


async def test_get_returns_single_budget() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/admin/budgets/t-acme").mock(
            return_value=httpx.Response(200, json=_budget_row())
        )
        result = await budgets.get("t-acme")
    assert result.tenant_id == "t-acme"
    assert result.remaining_usd == 87.5


async def test_get_rejects_empty_id() -> None:
    with pytest.raises(ValueError):
        await budgets.get("")


async def test_set_puts_payload_and_returns_budget() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.put(f"{BASE}/admin/budgets").mock(
            return_value=httpx.Response(200, json=_budget_row(monthly_usd=200.0))
        )
        result = await budgets.set(
            {"tenant_id": "t-acme", "monthly_usd": 200.0, "alert_threshold_pct": 90}
        )
    assert result.monthly_usd == 200.0
    body = route.calls.last.request.read().decode()
    assert "t-acme" in body
    assert "200" in body
    assert "alert_threshold_pct" in body


async def test_set_accepts_pydantic_input() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.put(f"{BASE}/admin/budgets").mock(
            return_value=httpx.Response(200, json=_budget_row(monthly_usd=50.0))
        )
        result = await budgets.set(SetBudgetInput(tenant_id="t-acme", monthly_usd=50.0))
    assert result.monthly_usd == 50.0
    body = route.calls.last.request.read().decode()
    # default threshold should be serialised (80.0).
    assert "alert_threshold_pct" in body


async def test_set_rejects_missing_tenant_id() -> None:
    with pytest.raises(ValueError):
        await budgets.set({"monthly_usd": 10.0})


async def test_set_rejects_non_positive_amount() -> None:
    with pytest.raises(ValueError):
        await budgets.set({"tenant_id": "t-acme", "monthly_usd": 0})
    with pytest.raises(ValueError):
        await budgets.set({"tenant_id": "t-acme", "monthly_usd": -5})


async def test_budget_exceeded_surfaces_as_error() -> None:
    """Server returns 402 + BUDGET_EXCEEDED on PUT against an over-budget tenant."""
    body = {
        "error": {
            "code": "BUDGET_EXCEEDED",
            "message": "tenant has exhausted its monthly budget",
        }
    }
    with respx.mock(assert_all_called=True) as router:
        router.put(f"{BASE}/admin/budgets").mock(return_value=httpx.Response(402, json=body))
        with pytest.raises(AFStackError) as exc:
            await budgets.set({"tenant_id": "t-acme", "monthly_usd": 10.0})
    assert exc.value.code == "BUDGET_EXCEEDED"
    assert exc.value.status_code == 402


async def test_suite_admin_budgets_namespace() -> None:
    assert suite.admin.budgets is budgets
    assert suite.admin.budgets.set is budgets.set
