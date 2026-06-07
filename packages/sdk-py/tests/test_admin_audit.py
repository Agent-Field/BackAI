"""HTTP-mocked tests for ``af_stack.admin.audit``."""

from __future__ import annotations

from datetime import UTC, datetime

import httpx
import pytest
import respx

from af_stack import suite
from af_stack._http import close as http_close
from af_stack.admin import audit

BASE = "http://localhost:8080/api/v1"


@pytest.fixture(autouse=True)
async def reset_http_client(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "test-key")
    await http_close()
    yield
    await http_close()


def _entry(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "a-1",
        "tenant_id": "t-1",
        "user_id": None,
        "api_key_id": "k-1",
        "action": "tenant.created",
        "resource_type": "tenant",
        "resource_id": "t-1",
        "metadata": {"slug": "acme"},
        "occurred_at": "2026-06-06T12:00:00Z",
    }
    base.update(overrides)
    return base


async def test_list_returns_page() -> None:
    page = {
        "entries": [_entry(), _entry(id="a-2", action="key.issued")],
        "total": 2,
        "has_more": False,
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/admin/audit").mock(
            return_value=httpx.Response(200, json=page)
        )
        result = await audit.list()
    assert result.total == 2
    assert result.has_more is False
    assert [e.id for e in result.entries] == ["a-1", "a-2"]
    assert result.entries[0].metadata == {"slug": "acme"}


async def test_list_forwards_filters() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/admin/audit").mock(
            return_value=httpx.Response(
                200, json={"entries": [], "total": 0, "has_more": False}
            )
        )
        await audit.list(
            tenant="t-1",
            action="tenant.created",
            from_="2026-06-01T00:00:00Z",
            to="2026-06-30T23:59:59Z",
            limit=10,
            offset=20,
        )
    params = route.calls.last.request.url.params
    assert params["tenant"] == "t-1"
    assert params["action"] == "tenant.created"
    assert params["from"] == "2026-06-01T00:00:00Z"
    assert params["to"] == "2026-06-30T23:59:59Z"
    assert params["limit"] == "10"
    assert params["offset"] == "20"


async def test_list_serialises_datetimes() -> None:
    when = datetime(2026, 6, 6, 12, 0, tzinfo=UTC)
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/admin/audit").mock(
            return_value=httpx.Response(
                200, json={"entries": [], "total": 0, "has_more": False}
            )
        )
        await audit.list(from_=when, to=when)
    params = route.calls.last.request.url.params
    assert "2026-06-06T12:00:00" in params["from"]
    assert "2026-06-06T12:00:00" in params["to"]


async def test_list_rejects_bad_paging() -> None:
    with pytest.raises(ValueError):
        await audit.list(limit=0)
    with pytest.raises(ValueError):
        await audit.list(offset=-1)


async def test_suite_namespace() -> None:
    assert suite.admin.audit is audit
