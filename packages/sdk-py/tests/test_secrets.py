# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for the ``af_stack.secrets`` module.

The main SDK exposes the full per-tenant verb set (get / reveal / list /
put / delete / rotate). All endpoints are authenticated and audited on
the runtime side.
"""

from __future__ import annotations

import json

import httpx
import pytest
import respx

from af_stack import secrets, suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close
from af_stack.secrets import SecretList, SecretMetadata

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
        route = router.post(f"{BASE}/vault/secrets/OPENAI_API_KEY/reveal").mock(
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
        route = router.post(f"{BASE}/vault/secrets/team%2Fdeploy-token/reveal").mock(
            return_value=httpx.Response(200, json={"key": "team/deploy-token", "value": "v"})
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
        router.post(f"{BASE}/vault/secrets/SOME_KEY/reveal").mock(
            return_value=httpx.Response(403, json=error_body)
        )
        with pytest.raises(AFStackError) as exc:
            await secrets.get("SOME_KEY")
    assert exc.value.code == "FORBIDDEN"
    assert exc.value.status_code == 403


async def test_get_raises_on_missing_value_field() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/vault/secrets/KEY/reveal").mock(
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


# ─── reveal (alias of get) ──────────────────────────────────────────────────


async def test_reveal_matches_get() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/vault/secrets/KEY/reveal").mock(
            return_value=httpx.Response(200, json={"key": "KEY", "value": "v"})
        )
        assert await secrets.reveal("KEY") == "v"


# ─── list ───────────────────────────────────────────────────────────────────


async def test_list_returns_metadata_rows() -> None:
    payload = {
        "secrets": [
            {
                "key": "OPENAI_API_KEY",
                "tenant_id": None,
                "description": "production",
                "rotate_after": None,
                "created_at": "2026-01-01T00:00:00Z",
                "updated_at": "2026-06-01T00:00:00Z",
            },
            {
                "key": "STRIPE_API_KEY",
                "tenant_id": "t_xyz",
                "description": None,
                "rotate_after": "2026-12-31T00:00:00Z",
                "created_at": "2026-01-01T00:00:00Z",
                "updated_at": "2026-06-01T00:00:00Z",
            },
        ]
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/vault/secrets").mock(return_value=httpx.Response(200, json=payload))
        result = await secrets.list()
    assert isinstance(result, SecretList)
    assert [row.key for row in result.secrets] == ["OPENAI_API_KEY", "STRIPE_API_KEY"]
    # No value field — the wire shape forbids it.
    for row in result.secrets:
        assert not hasattr(row, "value") or row.value is None  # type: ignore[attr-defined]


# ─── put ───────────────────────────────────────────────────────────────────


async def test_put_sends_value_and_returns_metadata() -> None:
    response = {
        "key": "NEW_KEY",
        "tenant_id": None,
        "description": "test",
        "rotate_after": None,
        "created_at": "2026-06-01T00:00:00Z",
        "updated_at": "2026-06-01T00:00:00Z",
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.put(f"{BASE}/vault/secrets/NEW_KEY").mock(
            return_value=httpx.Response(200, json=response)
        )
        meta = await secrets.put("NEW_KEY", "plaintext-value", description="test")
    assert isinstance(meta, SecretMetadata)
    body = json.loads(route.calls.last.request.read().decode())
    assert body["value"] == "plaintext-value"
    assert body["description"] == "test"
    # Optional fields omitted unless supplied.
    assert "rotate_after" not in body


async def test_put_rejects_empty_value() -> None:
    with pytest.raises(ValueError):
        await secrets.put("KEY", "")


# ─── delete ────────────────────────────────────────────────────────────────


async def test_delete_returns_true_on_success() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.delete(f"{BASE}/vault/secrets/KEY").mock(
            return_value=httpx.Response(200, json={"deleted": True})
        )
        assert await secrets.delete("KEY") is True


async def test_delete_returns_false_when_runtime_reports_no_op() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.delete(f"{BASE}/vault/secrets/MISSING").mock(
            return_value=httpx.Response(200, json={"deleted": False})
        )
        assert await secrets.delete("MISSING") is False


# ─── rotate ────────────────────────────────────────────────────────────────


async def test_rotate_posts_new_value_and_returns_metadata() -> None:
    response = {
        "key": "KEY",
        "tenant_id": None,
        "description": None,
        "rotate_after": None,
        "created_at": "2026-01-01T00:00:00Z",
        "updated_at": "2026-06-01T00:00:00Z",
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/vault/secrets/KEY/rotate").mock(
            return_value=httpx.Response(200, json=response)
        )
        meta = await secrets.rotate("KEY", "new-plaintext")
    assert isinstance(meta, SecretMetadata)
    body = json.loads(route.calls.last.request.read().decode())
    assert body == {"value": "new-plaintext"}


async def test_rotate_rejects_empty_value() -> None:
    with pytest.raises(ValueError):
        await secrets.rotate("KEY", "")
