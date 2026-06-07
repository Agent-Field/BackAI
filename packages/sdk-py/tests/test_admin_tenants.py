"""HTTP-mocked tests for ``af_stack.admin.tenants``.

Endpoint paths and JSON shapes mirror
``apps/dashboard/src/lib/api.ts`` (the canonical contract). The shared
httpx client is reset between tests so each test sees a clean client
bound to a deterministic base URL and API key.
"""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close
from af_stack.admin import tenants
from af_stack.admin._models import CreateTenantInput, UpdateTenantInput

BASE = "http://localhost:8080/api/v1"


def _tenant_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "t-1",
        "slug": "acme",
        "name": "Acme",
        "plan": "free",
        "settings": {},
        "quota": {},
        "created_at": "2026-06-06T12:00:00Z",
        "deleted_at": None,
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


async def test_list_returns_tenants() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/admin/tenants").mock(
            return_value=httpx.Response(
                200, json={"tenants": [_tenant_row(), _tenant_row(id="t-2", slug="beta")]}
            )
        )
        result = await tenants.list()
    assert [t.id for t in result.tenants] == ["t-1", "t-2"]
    assert result.tenants[0].slug == "acme"


async def test_get_returns_tenant_detail() -> None:
    detail = {
        "tenant": _tenant_row(),
        "members": [
            {
                "user": {
                    "id": "u-1",
                    "email": "owner@acme.co",
                    "name": "Owner",
                    "avatar_url": None,
                    "created_at": "2026-06-06T12:00:00Z",
                    "deleted_at": None,
                },
                "role": "owner",
            }
        ],
        "api_keys": [],
        "usage": {
            "requests_30d": 12,
            "cost_usd_30d": 1.25,
            "storage_bytes": 1024,
            "secrets_count": 3,
        },
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/admin/tenants/t-1").mock(
            return_value=httpx.Response(200, json=detail)
        )
        result = await tenants.get("t-1")
    assert result.tenant.id == "t-1"
    assert result.members[0].user.email == "owner@acme.co"
    assert result.usage.requests_30d == 12


async def test_get_rejects_empty_id() -> None:
    with pytest.raises(ValueError):
        await tenants.get("")


async def test_create_posts_payload_and_returns_tenant() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/admin/tenants").mock(
            return_value=httpx.Response(201, json=_tenant_row())
        )
        result = await tenants.create({"slug": "acme", "name": "Acme"})
    assert result.id == "t-1"
    body = route.calls.last.request.read().decode()
    assert "acme" in body
    assert "Acme" in body


async def test_create_accepts_pydantic_input() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/admin/tenants").mock(
            return_value=httpx.Response(201, json=_tenant_row(plan="pro"))
        )
        result = await tenants.create(
            CreateTenantInput(slug="acme", name="Acme", plan="pro")
        )
    assert result.plan == "pro"
    body = route.calls.last.request.read().decode()
    assert "pro" in body


async def test_update_patches_tenant() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.patch(f"{BASE}/admin/tenants/t-1").mock(
            return_value=httpx.Response(200, json=_tenant_row(name="Acme Renamed"))
        )
        result = await tenants.update(
            "t-1", UpdateTenantInput(name="Acme Renamed")
        )
    assert result.name == "Acme Renamed"
    body = route.calls.last.request.read().decode()
    # exclude_none should drop unset fields.
    assert "plan" not in body
    assert "settings" not in body


async def test_delete_returns_true() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.delete(f"{BASE}/admin/tenants/t-1").mock(
            return_value=httpx.Response(200, json={"deleted": True})
        )
        ok = await tenants.delete("t-1")
    assert ok is True


async def test_mt_disabled_surfaces_as_af_stack_error() -> None:
    """503 + MT_DISABLED envelope must surface as a clear AFStackError.

    This is the "multi-tenancy module not enabled" path the runtime
    returns when the module flag is off. The dashboard branches on
    ``code == "MT_DISABLED"`` to render the empty state.
    """
    body = {"error": {"code": "MT_DISABLED", "message": "multi-tenancy module not enabled"}}
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/admin/tenants").mock(
            return_value=httpx.Response(503, json=body)
        )
        with pytest.raises(AFStackError) as exc:
            await tenants.list()
    assert exc.value.code == "MT_DISABLED"
    assert exc.value.status_code == 503


async def test_suite_admin_tenants_namespace() -> None:
    assert suite.admin.tenants is tenants
    assert suite.admin.tenants.list is tenants.list
