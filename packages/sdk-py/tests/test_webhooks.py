"""HTTP-mocked tests for the ``af_stack.webhooks`` module.

Endpoint paths and JSON shapes mirror ``apps/dashboard/src/lib/api.ts``
(the canonical contract). The shared httpx client is reset between
tests so each test sees a clean client bound to a deterministic base
URL and API key.
"""

from __future__ import annotations

import json

import httpx
import pytest
import respx

from af_stack import suite, webhooks
from af_stack._http import AFStackError
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


def _delivery(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "wd_123",
        "endpoint_id": None,
        "direction": "outbound",
        "destination": "https://example.com/hooks",
        "event_type": "user.created",
        "status": "queued",
        "attempts": 0,
        "response_status": None,
        "response_ms": None,
        "body_preview": '{"name":"alice"}',
        "body_storage_url": None,
        "last_error": None,
        "scheduled_at": "2026-06-06T12:00:00Z",
        "delivered_at": None,
        "created_at": "2026-06-06T12:00:00Z",
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


# ─── send ─────────────────────────────────────────────────────────────────


async def test_send_posts_payload_with_defaults() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/webhooks/send").mock(
            return_value=httpx.Response(200, json=_delivery())
        )
        delivery = await webhooks.send(
            "https://example.com/hooks",
            "user.created",
            {"name": "alice"},
        )

    assert delivery.id == "wd_123"
    assert delivery.direction == "outbound"
    assert delivery.status == "queued"

    body = json.loads(route.calls.last.request.read().decode())
    assert body["url"] == "https://example.com/hooks"
    assert body["event_type"] == "user.created"
    assert body["body"] == {"name": "alice"}
    assert body["immediate"] is False
    assert "headers" not in body  # omitted when None


async def test_send_forwards_headers_and_immediate_flag() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/webhooks/send").mock(
            return_value=httpx.Response(
                200,
                json=_delivery(status="succeeded", attempts=1, response_status=200, response_ms=42),
            )
        )
        delivery = await webhooks.send(
            "https://example.com/hooks",
            "user.created",
            {"id": 1},
            headers={"X-Signature": "abc"},
            immediate=True,
        )

    assert delivery.status == "succeeded"
    assert delivery.response_status == 200
    body = json.loads(route.calls.last.request.read().decode())
    assert body["headers"] == {"X-Signature": "abc"}
    assert body["immediate"] is True


async def test_send_validates_url_and_event_type() -> None:
    with pytest.raises(ValueError):
        await webhooks.send("", "user.created", {})
    with pytest.raises(ValueError):
        await webhooks.send("https://example.com/hooks", "", {})


# ─── list ─────────────────────────────────────────────────────────────────


async def test_list_returns_paginated_deliveries() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/webhooks/deliveries").mock(
            return_value=httpx.Response(
                200,
                json={
                    "deliveries": [_delivery(), _delivery(id="wd_124", direction="inbound")],
                    "total": 2,
                    "has_more": False,
                },
            )
        )
        page = await webhooks.list()

    assert page.total == 2
    assert page.has_more is False
    assert len(page.deliveries) == 2
    assert page.deliveries[1].direction == "inbound"


async def test_list_passes_filters_as_query_params() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/webhooks/deliveries").mock(
            return_value=httpx.Response(
                200, json={"deliveries": [], "total": 0, "has_more": False}
            )
        )
        await webhooks.list(
            direction="outbound",
            status="failed",
            endpoint_id="ep_1",
            tenant="t_xyz",
            limit=10,
            offset=20,
        )

    params = dict(route.calls.last.request.url.params)
    assert params["direction"] == "outbound"
    assert params["status"] == "failed"
    assert params["endpoint_id"] == "ep_1"
    assert params["tenant"] == "t_xyz"
    assert params["limit"] == "10"
    assert params["offset"] == "20"


async def test_list_rejects_invalid_direction() -> None:
    with pytest.raises(ValueError):
        await webhooks.list(direction="sideways")


async def test_list_rejects_invalid_paging() -> None:
    with pytest.raises(ValueError):
        await webhooks.list(limit=0)
    with pytest.raises(ValueError):
        await webhooks.list(offset=-1)


# ─── get ──────────────────────────────────────────────────────────────────


async def test_get_returns_single_row() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/webhooks/deliveries/wd_123").mock(
            return_value=httpx.Response(200, json=_delivery())
        )
        row = await webhooks.get("wd_123")

    assert row.id == "wd_123"


async def test_get_validates_id() -> None:
    with pytest.raises(ValueError):
        await webhooks.get("")


async def test_get_raises_on_404() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/webhooks/deliveries/missing").mock(
            return_value=httpx.Response(
                404,
                json={"error": {"code": "NOT_FOUND", "message": "delivery missing"}},
            )
        )
        with pytest.raises(AFStackError) as exc:
            await webhooks.get("missing")
    assert exc.value.code == "NOT_FOUND"
    assert exc.value.status_code == 404


# ─── retry ────────────────────────────────────────────────────────────────


async def test_retry_resets_attempts_and_returns_row() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/webhooks/deliveries/wd_123/retry").mock(
            return_value=httpx.Response(
                200,
                json=_delivery(status="queued", attempts=0, last_error=None),
            )
        )
        row = await webhooks.retry("wd_123")

    assert row.status == "queued"
    assert row.attempts == 0
    assert row.last_error is None


async def test_retry_validates_id() -> None:
    with pytest.raises(ValueError):
        await webhooks.retry("")


# ─── suite namespace ─────────────────────────────────────────────────────


async def test_suite_namespace_exposes_webhooks() -> None:
    assert suite.webhooks is webhooks
