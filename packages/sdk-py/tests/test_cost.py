# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for ``af_stack.cost``.

Mirrors the canonical contract in
``apps/dashboard/src/lib/api.ts`` (Phase 7 cost events section).
"""

from __future__ import annotations

from datetime import UTC, datetime

import httpx
import pytest
import respx

from af_stack import cost, suite
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


def _event_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "ce-1",
        "tenant_id": "t-acme",
        "api_key_id": "key-abc",
        "model": "qwen/qwen-2.5-72b-instruct",
        "provider": "openrouter",
        "agent": "sample.summarize",
        "prompt_tokens": 100,
        "completion_tokens": 25,
        "total_tokens": 125,
        "cost_usd": 0.0001,
        "cached": False,
        "latency_ms": 410,
        "occurred_at": "2026-06-06T12:00:00Z",
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


async def test_events_returns_parsed_list() -> None:
    body = {
        "events": [_event_row(), _event_row(id="ce-2", cost_usd=0.0002)],
        "total": 42,
        "has_more": True,
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/cost/events").mock(return_value=httpx.Response(200, json=body))
        result = await cost.events()
    assert result.total == 42
    assert result.has_more is True
    assert [e.id for e in result.events] == ["ce-1", "ce-2"]
    assert result.events[0].cost_usd == 0.0001


async def test_events_sends_filters_as_query_params() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/cost/events").mock(
            return_value=httpx.Response(200, json={"events": [], "total": 0, "has_more": False})
        )
        await cost.events(
            tenant="t-acme",
            model="qwen/qwen-2.5-72b-instruct",
            limit=10,
            offset=5,
        )
    params = route.calls.last.request.url.params
    assert params["tenant"] == "t-acme"
    assert params["model"] == "qwen/qwen-2.5-72b-instruct"
    assert params["limit"] == "10"
    assert params["offset"] == "5"


async def test_events_serialises_datetime_filters() -> None:
    from_dt = datetime(2026, 6, 1, tzinfo=UTC)
    to_dt = datetime(2026, 6, 30)  # naive — should be treated as UTC
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/cost/events").mock(
            return_value=httpx.Response(200, json={"events": [], "total": 0, "has_more": False})
        )
        await cost.events(from_=from_dt, to=to_dt)
    params = route.calls.last.request.url.params
    assert "2026-06-01" in params["from"]
    assert params["to"].endswith("Z")


async def test_events_rejects_invalid_pagination() -> None:
    with pytest.raises(ValueError):
        await cost.events(limit=0)
    with pytest.raises(ValueError):
        await cost.events(offset=-1)


async def test_suite_cost_namespace() -> None:
    assert suite.cost is cost
    assert suite.cost.events is cost.events
