# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for ``af_stack.admin.users``."""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import suite
from af_stack._http import close as http_close
from af_stack.admin import users

BASE = "http://localhost:8080/api/v1"


@pytest.fixture(autouse=True)
async def reset_http_client(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "test-key")
    await http_close()
    yield
    await http_close()


def _user_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "u-1",
        "email": "a@b.co",
        "name": "Alice",
        "avatar_url": None,
        "created_at": "2026-06-06T12:00:00Z",
        "deleted_at": None,
    }
    base.update(overrides)
    return base


async def test_list_no_filters() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/admin/users").mock(
            return_value=httpx.Response(
                200, json={"users": [_user_row(), _user_row(id="u-2", email="b@b.co")]}
            )
        )
        result = await users.list()
    assert [u.id for u in result.users] == ["u-1", "u-2"]
    # No filters -> no query params.
    params = route.calls.last.request.url.params
    assert "tenant" not in params
    assert "search" not in params


async def test_list_forwards_filters() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/admin/users").mock(
            return_value=httpx.Response(200, json={"users": []})
        )
        await users.list(tenant="t-1", search="alice")
    params = route.calls.last.request.url.params
    assert params["tenant"] == "t-1"
    assert params["search"] == "alice"


async def test_suite_namespace() -> None:
    assert suite.admin.users is users