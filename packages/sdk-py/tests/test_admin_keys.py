"""HTTP-mocked tests for ``af_stack.admin.keys``."""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import suite
from af_stack._http import close as http_close
from af_stack.admin import keys
from af_stack.admin._models import IssueAPIKeyInput

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
        "id": "k-1",
        "tenant_id": "t-1",
        "prefix": "abc12345",
        "name": "ci-bot",
        "scopes": ["read"],
        "created_by": None,
        "created_at": "2026-06-06T12:00:00Z",
        "last_used_at": None,
        "expires_at": None,
        "revoked_at": None,
    }
    base.update(overrides)
    return base


async def test_list_no_filters() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/admin/keys").mock(
            return_value=httpx.Response(200, json={"keys": [_row()]})
        )
        result = await keys.list()
    assert result.keys[0].id == "k-1"


async def test_list_with_tenant_filter() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/admin/keys").mock(
            return_value=httpx.Response(200, json={"keys": []})
        )
        await keys.list(tenant="t-1")
    params = route.calls.last.request.url.params
    assert params["tenant"] == "t-1"


async def test_issue_returns_value_once() -> None:
    """``IssuedAPIKey.value`` round-trip — the one-time-reveal contract.

    POST /api/v1/admin/keys returns the plaintext ``value`` exactly once.
    A subsequent list / get must NOT include it (validated separately in
    ``test_list_does_not_carry_value``).
    """
    issued = _row()
    issued["value"] = "af_abc12345_supersecret"  # only present on issue
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/admin/keys").mock(
            return_value=httpx.Response(201, json=issued)
        )
        result = await keys.issue({"tenant_id": "t-1", "scopes": ["read"]})
    assert result.value == "af_abc12345_supersecret"
    assert result.id == "k-1"
    assert result.tenant_id == "t-1"


async def test_issue_accepts_pydantic_input() -> None:
    issued = _row()
    issued["value"] = "af_xx_yy"
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/admin/keys").mock(
            return_value=httpx.Response(201, json=issued)
        )
        result = await keys.issue(
            IssueAPIKeyInput(tenant_id="t-1", name="ci-bot", scopes=["read", "write"])
        )
    assert result.value == "af_xx_yy"
    body = route.calls.last.request.read().decode()
    assert "ci-bot" in body
    assert "read" in body and "write" in body


async def test_list_does_not_carry_value() -> None:
    """The list response omits ``value`` — APIKey, not IssuedAPIKey.

    Pydantic's ``APIKey`` model has no ``value`` field, so even if the
    server accidentally returned one it'd be silently dropped (with
    ``extra='allow'`` it would land on the object but not the typed
    attribute). This test pins that we never *expect* to read it back.
    """
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/admin/keys").mock(
            return_value=httpx.Response(200, json={"keys": [_row()]})
        )
        result = await keys.list()
    # No attribute access — the typed model exposes nothing called
    # ``value`` for plain API keys.
    assert "value" not in result.keys[0].model_dump()


async def test_issue_rejects_missing_tenant_id() -> None:
    with pytest.raises(ValueError):
        await keys.issue({"scopes": []})


async def test_revoke_returns_true() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.delete(f"{BASE}/admin/keys/k-1").mock(
            return_value=httpx.Response(200, json={"revoked": True})
        )
        ok = await keys.revoke("k-1")
    assert ok is True


async def test_revoke_rejects_empty_id() -> None:
    with pytest.raises(ValueError):
        await keys.revoke("")


async def test_suite_namespace() -> None:
    assert suite.admin.keys is keys
