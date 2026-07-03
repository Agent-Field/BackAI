# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for ``af_stack.billing``.

Mirrors the canonical contract in ``apps/dashboard/src/lib/api.ts``
(Phase 10.4 billing section).
"""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import billing, suite
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


def _customer_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "tenant_id": "00000000-0000-0000-0000-000000000001",
        "stripe_customer_id": "cus_stub_acme",
        "email": "billing@acme.test",
        "plan": "pro",
        "trial_ends_at": None,
        "current_period_end": "2026-07-01T00:00:00Z",
        "subscription_status": "active",
        "created_at": "2026-05-01T00:00:00Z",
        "updated_at": "2026-06-01T00:00:00Z",
    }
    base.update(overrides)
    return base


def _meter_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "meter": "sandbox_seconds",
        "tenant_id": "00000000-0000-0000-0000-000000000001",
        "period_start": "2026-06-01T00:00:00Z",
        "period_end": "2026-07-01T00:00:00Z",
        "quantity": 1234.0,
        "cost_usd": 0.0617,
        "stripe_meter_id": None,
        "last_synced_at": None,
    }
    base.update(overrides)
    return base


def _plan_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "pro",
        "name": "Pro",
        "stripe_price_id": "price_1QStubPro",
        "price_usd_month": 29.0,
        "llm_budget_usd": 50.0,
        "entitlements": {"max_agents": 10, "priority_support": True},
        "is_default": False,
        "created_at": "2026-07-01T00:00:00Z",
        "updated_at": "2026-07-01T00:00:00Z",
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


async def test_customers_returns_parsed_list() -> None:
    body = {
        "customers": [
            _customer_row(),
            _customer_row(
                tenant_id="00000000-0000-0000-0000-000000000002",
                plan="free",
                subscription_status=None,
            ),
        ],
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/billing/customers").mock(return_value=httpx.Response(200, json=body))
        result = await billing.customers()
    assert len(result.customers) == 2
    assert result.customers[0].plan == "pro"
    assert result.customers[1].plan == "free"


async def test_customer_returns_single_row() -> None:
    body = _customer_row()
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/billing/customers/00000000-0000-0000-0000-000000000001").mock(
            return_value=httpx.Response(200, json=body)
        )
        result = await billing.customer("00000000-0000-0000-0000-000000000001")
    assert result.tenant_id == "00000000-0000-0000-0000-000000000001"
    assert result.plan == "pro"
    assert result.stripe_customer_id == "cus_stub_acme"


async def test_customer_rejects_empty_tenant() -> None:
    with pytest.raises(ValueError):
        await billing.customer("")


async def test_meters_returns_parsed_list() -> None:
    body = {
        "meters": [
            _meter_row(),
            _meter_row(meter="llm_tokens", quantity=4321.0, cost_usd=None),
        ],
        "total_cost_usd": 0.0617,
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/billing/meters").mock(return_value=httpx.Response(200, json=body))
        result = await billing.meters()
    assert result.total_cost_usd == 0.0617
    assert len(result.meters) == 2
    assert result.meters[0].meter == "sandbox_seconds"
    assert result.meters[1].cost_usd is None


async def test_meters_sends_query_params() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/billing/meters").mock(
            return_value=httpx.Response(200, json={"meters": [], "total_cost_usd": 0.0})
        )
        await billing.meters(
            tenant="00000000-0000-0000-0000-000000000001",
            period_start="2026-06-01T00:00:00Z",
            bucket="day",
        )
    params = route.calls.last.request.url.params
    assert params["tenant"] == "00000000-0000-0000-0000-000000000001"
    assert params["period_start"] == "2026-06-01T00:00:00Z"
    assert params["bucket"] == "day"


async def test_portal_link_returns_url() -> None:
    body = {
        "url": "https://example.com/portal-stub?customer=cus_stub_acme",
        "expires_at": "2026-06-02T00:00:00Z",
    }
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/billing/customers/00000000-0000-0000-0000-000000000001/portal").mock(
            return_value=httpx.Response(200, json=body)
        )
        result = await billing.portal_link(
            "00000000-0000-0000-0000-000000000001",
            return_url="https://app.example.com/back",
        )
    assert result.url.startswith("https://example.com/portal-stub")
    assert result.expires_at == "2026-06-02T00:00:00Z"


async def test_portal_link_rejects_empty_tenant() -> None:
    with pytest.raises(ValueError):
        await billing.portal_link("")


async def test_meter_validates_inputs() -> None:
    # Bad inputs raise before any HTTP call.
    with pytest.raises(ValueError):
        await billing.meter("", 1.0)
    with pytest.raises(ValueError):
        await billing.meter("sandbox_seconds", -1.0)


async def test_meter_posts_increment() -> None:
    import json as _json

    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/billing/meter").mock(return_value=httpx.Response(204))
        result = await billing.meter("sandbox_seconds", 1.5, tenant_id="t1")

    assert result is None
    assert route.called
    sent = _json.loads(route.calls[0].request.content)
    assert sent == {"name": "sandbox_seconds", "qty": 1.5, "tenant_id": "t1"}


async def test_has_budget_is_permissive_for_unknown_plan() -> None:
    body_cust = _customer_row(plan="enterprise")
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/billing/customers/00000000-0000-0000-0000-000000000001").mock(
            return_value=httpx.Response(200, json=body_cust)
        )
        ok = await billing.has_budget("00000000-0000-0000-0000-000000000001", 100.0)
    assert ok is True


async def test_has_budget_blocks_over_cap() -> None:
    body_cust = _customer_row(plan="free")
    body_meters = {
        "meters": [_meter_row(cost_usd=9.5)],
        "total_cost_usd": 9.5,
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/billing/customers/00000000-0000-0000-0000-000000000001").mock(
            return_value=httpx.Response(200, json=body_cust)
        )
        router.get(f"{BASE}/billing/meters").mock(
            return_value=httpx.Response(200, json=body_meters)
        )
        # free cap is $10. existing $9.5 + additional $1.0 = $10.5 > cap.
        ok = await billing.has_budget("00000000-0000-0000-0000-000000000001", 1.0)
    assert ok is False


async def test_has_budget_passes_within_cap() -> None:
    body_cust = _customer_row(plan="free")
    body_meters = {
        "meters": [_meter_row(cost_usd=5.0)],
        "total_cost_usd": 5.0,
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/billing/customers/00000000-0000-0000-0000-000000000001").mock(
            return_value=httpx.Response(200, json=body_cust)
        )
        router.get(f"{BASE}/billing/meters").mock(
            return_value=httpx.Response(200, json=body_meters)
        )
        ok = await billing.has_budget("00000000-0000-0000-0000-000000000001", 1.0)
    assert ok is True


async def test_has_budget_permissive_without_tenant() -> None:
    # No HTTP calls expected.
    assert await billing.has_budget("", 100.0) is True


async def test_plans_returns_parsed_catalog() -> None:
    body = {
        "plans": [
            _plan_row(
                id="free",
                name="Free",
                stripe_price_id=None,
                price_usd_month=0.0,
                llm_budget_usd=None,
                entitlements={},
                is_default=True,
            ),
            _plan_row(),
        ],
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/billing/plans").mock(return_value=httpx.Response(200, json=body))
        result = await billing.plans()
    assert len(result.plans) == 2
    free, pro = result.plans
    assert free.id == "free"
    assert free.is_default is True
    assert free.stripe_price_id is None
    assert free.llm_budget_usd is None
    assert free.entitlements == {}
    assert pro.id == "pro"
    assert pro.price_usd_month == 29.0
    assert pro.llm_budget_usd == 50.0
    assert pro.entitlements == {"max_agents": 10, "priority_support": True}


async def test_checkout_posts_minimal_payload() -> None:
    import json as _json

    body = {"url": "https://checkout.stripe.com/c/pay/cs_test_123", "applied_directly": False}
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/billing/checkout").mock(
            return_value=httpx.Response(200, json=body)
        )
        result = await billing.checkout("pro", "https://app.example.com/thanks")
    sent = _json.loads(route.calls[0].request.content)
    # Optional fields stay off the wire when not given.
    assert sent == {"plan_id": "pro", "success_url": "https://app.example.com/thanks"}
    assert result.url == "https://checkout.stripe.com/c/pay/cs_test_123"
    assert result.applied_directly is False


async def test_checkout_sends_optional_fields() -> None:
    import json as _json

    body = {"url": "https://checkout.stripe.com/c/pay/cs_test_456", "applied_directly": False}
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/billing/checkout").mock(
            return_value=httpx.Response(200, json=body)
        )
        await billing.checkout(
            "pro",
            "https://app.example.com/thanks",
            cancel_url="https://app.example.com/pricing",
            tenant_id="00000000-0000-0000-0000-000000000001",
        )
    sent = _json.loads(route.calls[0].request.content)
    assert sent == {
        "plan_id": "pro",
        "success_url": "https://app.example.com/thanks",
        "cancel_url": "https://app.example.com/pricing",
        "tenant_id": "00000000-0000-0000-0000-000000000001",
    }


async def test_checkout_stub_mode_applies_directly() -> None:
    # Stub/dev runtime: plan applied immediately, no redirect URL.
    body = {"url": "", "applied_directly": True}
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/billing/checkout").mock(return_value=httpx.Response(200, json=body))
        result = await billing.checkout("pro", "https://app.example.com/thanks")
    assert result.applied_directly is True
    assert result.url == ""


async def test_checkout_rejects_empty_inputs() -> None:
    # Bad inputs raise before any HTTP call.
    with pytest.raises(ValueError):
        await billing.checkout("", "https://app.example.com/thanks")
    with pytest.raises(ValueError):
        await billing.checkout("pro", "")


async def test_entitlements_omits_tenant_param_when_not_given() -> None:
    body = {
        "tenant_id": "00000000-0000-0000-0000-000000000001",
        "plan": _plan_row(),
        "entitlements": {"max_agents": 10, "priority_support": True},
        "usage": {"sandbox_seconds": 1234.0, "llm_tokens": 4321.0},
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/billing/entitlements").mock(
            return_value=httpx.Response(200, json=body)
        )
        result = await billing.entitlements()
    assert "tenant" not in route.calls.last.request.url.params
    assert result.tenant_id == "00000000-0000-0000-0000-000000000001"
    assert result.plan.id == "pro"
    assert result.entitlements == {"max_agents": 10, "priority_support": True}
    assert result.usage == {"sandbox_seconds": 1234.0, "llm_tokens": 4321.0}


async def test_entitlements_sends_tenant_param() -> None:
    body = {
        "tenant_id": "00000000-0000-0000-0000-000000000002",
        "plan": _plan_row(id="free", name="Free", is_default=True),
        "entitlements": {},
        "usage": {},
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/billing/entitlements").mock(
            return_value=httpx.Response(200, json=body)
        )
        result = await billing.entitlements(tenant="00000000-0000-0000-0000-000000000002")
    params = route.calls.last.request.url.params
    assert params["tenant"] == "00000000-0000-0000-0000-000000000002"
    assert result.usage == {}
    assert result.entitlements == {}


async def test_suite_billing_namespace() -> None:
    assert suite.billing is billing
    assert suite.billing.customers is billing.customers
    assert suite.billing.portal_link is billing.portal_link
    assert suite.billing.plans is billing.plans
    assert suite.billing.checkout is billing.checkout
    assert suite.billing.entitlements is billing.entitlements
