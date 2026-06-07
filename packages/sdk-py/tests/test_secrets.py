# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for the ``af_stack.secrets`` module.

Only :func:`secrets.get` exists in the main SDK; admin verbs are post-v1.
"""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import secrets, suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


@pytest.fixture(autouse=True)
async def reset_http_client(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "test-key")
    await http_close()
    yield
    await http_close()


async def test_get_returns_plaintext_value() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/secrets/OPENAI_API_KEY/reveal").mock(
            return_value=httpx.Response(
                200,
                json={"key": "OPENAI_API_KEY", "value": "sk-very-secret"},
            )
        )
        value = await secrets.get("OPENAI_API_KEY")
    assert value == "sk-very-secret"
    assert route.calls.last.request.method == "POST"
    assert route.calls.last.request.headers["authorization"] == "Bearer test-key"


async def test_get_url_encodes_key() -> None:
    # Keys can contain characters that need percent-encoding in URL paths
    # (e.g. slashes for namespaced names).
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/secrets/team%2Fdeploy-token/reveal").mock(
            return_value=httpx.Response(
                200, json={"key": "team/deploy-token", "value": "v"}
            )
        )
        value = await secrets.get("team/deploy-token")
    assert value == "v"
    assert "team%2Fdeploy-token" in str(route.calls.last.request.url)


async def test_get_raises_on_unauthorised() -> None:
    error_body = {
        "error": {
            "code": "FORBIDDEN",
            "message": "not authorised to reveal this secret",
            "request_id": "req_x",
        }
    }
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/secrets/SOME_KEY/reveal").mock(
            return_value=httpx.Response(403, json=error_body)
        )
        with pytest.raises(AFStackError) as exc:
            await secrets.get("SOME_KEY")
    assert exc.value.code == "FORBIDDEN"
    assert exc.value.status_code == 403


async def test_get_raises_on_missing_value_field() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/secrets/KEY/reveal").mock(
            return_value=httpx.Response(200, json={"key": "KEY"})
        )
        with pytest.raises(AFStackError) as exc:
            await secrets.get("KEY")
    assert exc.value.code == "BAD_RESPONSE"


async def test_get_rejects_empty_key() -> None:
    with pytest.raises(ValueError):
        await secrets.get("")


async def test_suite_secrets_namespace_matches_module() -> None:
    assert suite.secrets is secrets
    assert suite.secrets.get is secrets.get


def test_admin_verbs_not_exposed() -> None:
    # Main SDK is read-only by design; admin operations land in the
    # post-v1 admin SDK. Guard against accidental re-export.
    for verb in ("set", "delete", "rotate", "list", "put"):
        assert not hasattr(secrets, verb), (
            f"secrets.{verb} should live in the admin SDK, not the main SDK"
        )