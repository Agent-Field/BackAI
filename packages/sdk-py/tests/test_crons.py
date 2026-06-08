# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked contract tests for the ``af_stack.crons`` module.

Verifies request shape and response parsing against the wire contract in
``services/runtime/internal/server/crons.go``.
"""

from __future__ import annotations

import json

import httpx
import pytest
import respx

from af_stack import crons, suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close
from af_stack.crons import Cron, CronList

BASE = "http://localhost:8080/api/v1"


def _row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "cron_abc",
        "tenant_id": None,
        "name": "nightly-rollup",
        "job_name": "rollup",
        "schedule": "0 0 * * *",
        "args": {"window": "24h"},
        "is_active": True,
        "last_run_at": None,
        "next_run_at": "2026-06-08T00:00:00Z",
        "created_at": "2026-06-01T00:00:00Z",
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


# ─── list ──────────────────────────────────────────────────────────────────


async def test_list_parses_rows() -> None:
    payload = {"crons": [_row(), _row(id="cron_def", name="weekly")]}
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/crons").mock(
            return_value=httpx.Response(200, json=payload)
        )
        result = await crons.list()
    assert isinstance(result, CronList)
    assert [c.id for c in result.crons] == ["cron_abc", "cron_def"]
    assert "tenant" not in route.calls.last.request.url.params


async def test_list_passes_tenant_filter() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/crons").mock(
            return_value=httpx.Response(200, json={"crons": []})
        )
        await crons.list(tenant="t_xyz")
    params = route.calls.last.request.url.params
    assert params["tenant"] == "t_xyz"


async def test_list_handles_empty_body() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/crons").mock(
            return_value=httpx.Response(200, json={"crons": []})
        )
        result = await crons.list()
    assert result.crons == []


# ─── create ────────────────────────────────────────────────────────────────


async def test_create_posts_payload_and_parses_row() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/crons").mock(
            return_value=httpx.Response(200, json=_row())
        )
        result = await crons.create(
            "nightly-rollup",
            "rollup",
            "0 0 * * *",
            args={"window": "24h"},
            is_active=True,
        )

    assert isinstance(result, Cron)
    assert result.name == "nightly-rollup"
    body = json.loads(route.calls.last.request.read().decode())
    assert body == {
        "name": "nightly-rollup",
        "job_name": "rollup",
        "schedule": "0 0 * * *",
        "args": {"window": "24h"},
        "is_active": True,
    }


async def test_create_omits_optional_fields_when_not_supplied() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/crons").mock(
            return_value=httpx.Response(200, json=_row())
        )
        await crons.create("daily", "rollup", "0 6 * * *")
    body = json.loads(route.calls.last.request.read().decode())
    assert body == {
        "name": "daily",
        "job_name": "rollup",
        "schedule": "0 6 * * *",
    }


async def test_create_validates_inputs() -> None:
    with pytest.raises(ValueError):
        await crons.create("", "j", "0 * * * *")
    with pytest.raises(ValueError):
        await crons.create("n", "", "0 * * * *")
    with pytest.raises(ValueError):
        await crons.create("n", "j", "")


async def test_create_raises_on_not_configured() -> None:
    error_body = {
        "error": {
            "code": "CRONS_NOT_CONFIGURED",
            "message": "crons module is not configured on this runtime",
            "request_id": "req_x",
        }
    }
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/crons").mock(
            return_value=httpx.Response(503, json=error_body)
        )
        with pytest.raises(AFStackError) as exc:
            await crons.create("n", "j", "0 * * * *")
    assert exc.value.code == "CRONS_NOT_CONFIGURED"
    assert exc.value.status_code == 503


# ─── get ───────────────────────────────────────────────────────────────────


async def test_get_returns_single_row() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/crons/cron_abc").mock(
            return_value=httpx.Response(200, json=_row())
        )
        result = await crons.get("cron_abc")
    assert result.id == "cron_abc"


async def test_get_rejects_empty_id() -> None:
    with pytest.raises(ValueError):
        await crons.get("")


# ─── set_active ────────────────────────────────────────────────────────────


async def test_set_active_puts_flag() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.put(f"{BASE}/crons/cron_abc/active").mock(
            return_value=httpx.Response(200, json=_row(is_active=False))
        )
        result = await crons.set_active("cron_abc", False)
    assert result.is_active is False
    body = json.loads(route.calls.last.request.read().decode())
    assert body == {"is_active": False}


async def test_set_active_rejects_empty_id() -> None:
    with pytest.raises(ValueError):
        await crons.set_active("", True)


# ─── delete ────────────────────────────────────────────────────────────────


async def test_delete_returns_true_on_success() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.delete(f"{BASE}/crons/cron_abc").mock(
            return_value=httpx.Response(200, json={"deleted": True})
        )
        assert await crons.delete("cron_abc") is True


async def test_delete_rejects_empty_id() -> None:
    with pytest.raises(ValueError):
        await crons.delete("")


# ─── namespace ─────────────────────────────────────────────────────────────


async def test_suite_crons_namespace_matches_module() -> None:
    assert suite.crons is crons
    assert suite.crons.create is crons.create
    assert suite.crons.list is crons.list
