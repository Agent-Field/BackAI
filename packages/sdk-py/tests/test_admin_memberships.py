# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for ``af_stack.admin.memberships``."""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close
from af_stack.admin import memberships

BASE = "http://localhost:8080/api/v1"


@pytest.fixture(autouse=True)
async def reset_http_client(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "test-key")
    await http_close()
    yield
    await http_close()


def _row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "tenant_id": "t-1",
        "user_id": "u-1",
        "role": "owner",
        "invited_at": "2026-06-06T12:00:00Z",
        "accepted_at": "2026-06-06T12:00:01Z",
    }
    base.update(overrides)
    return base


async def test_list_no_filters() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/admin/memberships").mock(
            return_value=httpx.Response(200, json={"memberships": [_row()]})
        )
        result = await memberships.list()
    assert result.memberships[0].tenant_id == "t-1"


async def test_list_forwards_filters() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/admin/memberships").mock(
            return_value=httpx.Response(200, json={"memberships": []})
        )
        await memberships.list(tenant="t-1", user="u-1")
    params = route.calls.last.request.url.params
    assert params["tenant"] == "t-1"
    assert params["user"] == "u-1"


async def test_add_posts_body_and_parses_membership() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/admin/memberships").mock(
            return_value=httpx.Response(201, json=_row(role="admin"))
        )
        m = await memberships.add("t-1", "u-1", "admin")
    assert m.role == "admin"
    body = route.calls.last.request.read().decode()
    assert "tenant_id" in body
    assert "user_id" in body
    assert "admin" in body


async def test_add_rejects_empty_inputs() -> None:
    with pytest.raises(ValueError):
        await memberships.add("", "u-1", "member")
    with pytest.raises(ValueError):
        await memberships.add("t-1", "", "member")
    with pytest.raises(ValueError):
        await memberships.add("t-1", "u-1", "")


async def test_remove_deletes_and_returns_true() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.delete(f"{BASE}/admin/memberships/t-1/u-1").mock(
            return_value=httpx.Response(200, json={"deleted": True})
        )
        ok = await memberships.remove("t-1", "u-1")
    assert ok is True


async def test_remove_surfaces_not_found() -> None:
    body = {"error": {"code": "NOT_FOUND", "message": "no such membership"}}
    with respx.mock(assert_all_called=True) as router:
        router.delete(f"{BASE}/admin/memberships/t-1/u-1").mock(
            return_value=httpx.Response(404, json=body)
        )
        with pytest.raises(AFStackError) as exc:
            await memberships.remove("t-1", "u-1")
    assert exc.value.code == "NOT_FOUND"


async def test_suite_namespace() -> None:
    assert suite.admin.memberships is memberships
