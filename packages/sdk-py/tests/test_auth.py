# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for ``af_stack.auth`` (suite.auth.whoami)."""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import auth, suite
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


@pytest.fixture(autouse=True)
async def reset_http_client(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "test-key")
    await http_close()
    yield
    await http_close()


async def test_whoami_parses_identity() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/auth/whoami").mock(
            return_value=httpx.Response(
                200,
                json={
                    "authenticated": True,
                    "tenant_id": "t1",
                    "user_id": "u1",
                    "api_key_id": "k1",
                },
            )
        )
        me = await auth.whoami()

    assert me.authenticated is True
    assert me.tenant_id == "t1"
    assert me.user_id == "u1"
    assert me.api_key_id == "k1"


async def test_whoami_handles_unauthenticated() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/auth/whoami").mock(
            return_value=httpx.Response(
                200,
                json={
                    "authenticated": False,
                    "tenant_id": "",
                    "user_id": "",
                    "api_key_id": "",
                },
            )
        )
        me = await auth.whoami()

    assert me.authenticated is False
    assert me.tenant_id == ""


def test_suite_exposes_auth() -> None:
    assert suite.auth.whoami is auth.whoami
