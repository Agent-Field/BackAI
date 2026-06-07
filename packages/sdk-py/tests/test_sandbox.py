"""HTTP-mocked tests for the ``af_stack.sandbox`` module.

Endpoint paths and JSON shapes mirror ``apps/dashboard/src/lib/api.ts``
(the canonical contract). The shared httpx client is reset between
tests so each test sees a clean client bound to a deterministic base
URL and API key.
"""

from __future__ import annotations

import json

import httpx
import pytest
import respx

from af_stack import sandbox, suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


def _run_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "sbx_123",
        "tenant_id": "t_xyz",
        "workspace_id": None,
        "adapter": "docker",
        "image": "python:3.12-slim",
        "command": ["python", "-c", "print('hi')"],
        "status": "done",
        "exit_code": 0,
        "duration_s": 1.42,
        "cpu_seconds": 0.61,
        "memory_peak_mb": 32.5,
        "network_bytes_in": 0,
        "network_bytes_out": 0,
        "cost_usd": 0.0003,
        "stdout_url": "https://example.com/stdout",
        "stderr_url": "https://example.com/stderr",
        "artifacts_url": None,
        "started_at": "2026-06-06T12:00:00Z",
        "ended_at": "2026-06-06T12:00:01Z",
        "created_at": "2026-06-06T12:00:00Z",
    }
    base.update(overrides)
    return base


def _capabilities() -> dict[str, object]:
    return {
        "max_timeout_s": 86400,
        "supports_gpu": False,
        "supports_network": True,
        "supports_mounts": True,
        "cold_start_ms": 250,
        "adapter": "docker",
    }


@pytest.fixture(autouse=True)
async def reset_http_client(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "test-key")
    await http_close()
    yield
    await http_close()


# ─── run ──────────────────────────────────────────────────────────────────


async def test_run_posts_payload_with_defaults() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/sandbox/run").mock(
            return_value=httpx.Response(200, json=_run_row(status="queued", exit_code=None))
        )
        run_row = await sandbox.run("python:3.12-slim", ["python", "-c", "print(1)"])

    assert run_row.id == "sbx_123"
    assert run_row.status == "queued"
    assert run_row.adapter == "docker"

    body = json.loads(route.calls.last.request.read().decode())
    assert body["image"] == "python:3.12-slim"
    assert body["command"] == ["python", "-c", "print(1)"]
    # Default values from the SandboxRunInput contract.
    assert body["timeout_s"] == 300
    assert body["cpu"] == 2
    assert body["memory_gb"] == 4
    assert body["network"] == "restricted"
    # Optional fields must NOT appear when caller didn't supply them.
    assert "files" not in body
    assert "env" not in body
    assert "allow_egress" not in body
    assert "workspace_id" not in body


async def test_run_overrides_resource_and_network_caps() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/sandbox/run").mock(
            return_value=httpx.Response(200, json=_run_row())
        )
        await sandbox.run(
            "python:3.12-slim",
            ["python", "-c", "1"],
            timeout_s=60,
            cpu=8,
            memory_gb=16,
            network="isolated",
            allow_egress=["api.example.com"],
            workspace_id="ws_abc",
        )

    body = json.loads(route.calls.last.request.read().decode())
    assert body["timeout_s"] == 60
    assert body["cpu"] == 8
    assert body["memory_gb"] == 16
    assert body["network"] == "isolated"
    assert body["allow_egress"] == ["api.example.com"]
    assert body["workspace_id"] == "ws_abc"


async def test_run_forwards_files_and_env_dicts() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/sandbox/run").mock(
            return_value=httpx.Response(200, json=_run_row())
        )
        await sandbox.run(
            "python:3.12-slim",
            ["python", "main.py"],
            files={"main.py": "print('hi')", "data.txt": "hello"},
            env={"PYTHONUNBUFFERED": "1", "MODE": "test"},
        )

    body = json.loads(route.calls.last.request.read().decode())
    assert body["files"] == {"main.py": "print('hi')", "data.txt": "hello"}
    assert body["env"] == {"PYTHONUNBUFFERED": "1", "MODE": "test"}


async def test_run_validates_inputs() -> None:
    with pytest.raises(ValueError):
        await sandbox.run("", ["python"])
    with pytest.raises(ValueError):
        await sandbox.run("python:3.12-slim", [])
    with pytest.raises(ValueError):
        await sandbox.run("python:3.12-slim", ["python"], timeout_s=0)
    with pytest.raises(ValueError):
        await sandbox.run("python:3.12-slim", ["python"], timeout_s=999_999)
    with pytest.raises(ValueError):
        await sandbox.run("python:3.12-slim", ["python"], cpu=0)
    with pytest.raises(ValueError):
        await sandbox.run("python:3.12-slim", ["python"], cpu=999)
    with pytest.raises(ValueError):
        await sandbox.run("python:3.12-slim", ["python"], memory_gb=0)
    with pytest.raises(ValueError):
        await sandbox.run("python:3.12-slim", ["python"], memory_gb=999)
    with pytest.raises(ValueError):
        await sandbox.run("python:3.12-slim", ["python"], network="bogus")


async def test_run_raises_on_structured_error() -> None:
    error_body = {
        "error": {
            "code": "SANDBOX_QUOTA_EXCEEDED",
            "message": "tenant out of sandbox quota",
            "request_id": "req_x",
        }
    }
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/sandbox/run").mock(
            return_value=httpx.Response(429, json=error_body)
        )
        with pytest.raises(AFStackError) as exc:
            await sandbox.run("python:3.12-slim", ["python", "-c", "1"])
    assert exc.value.code == "SANDBOX_QUOTA_EXCEEDED"
    assert exc.value.status_code == 429


# ─── list ─────────────────────────────────────────────────────────────────


async def test_list_sends_filters_as_query_params() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/sandbox/runs").mock(
            return_value=httpx.Response(
                200,
                json={
                    "runs": [_run_row(id="sbx_1"), _run_row(id="sbx_2")],
                    "total": 17,
                    "has_more": True,
                },
            )
        )
        lst = await sandbox.list(
            tenant="t_xyz",
            status="running",
            limit=10,
            offset=0,
        )

    assert lst.total == 17
    assert lst.has_more is True
    assert [r.id for r in lst.runs] == ["sbx_1", "sbx_2"]

    params = route.calls.last.request.url.params
    assert params["tenant"] == "t_xyz"
    assert params["status"] == "running"
    assert params["limit"] == "10"
    assert params["offset"] == "0"


async def test_list_omits_unset_filters_and_handles_empty_response() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/sandbox/runs").mock(
            return_value=httpx.Response(
                200, json={"runs": [], "total": 0, "has_more": False}
            )
        )
        lst = await sandbox.list()

    assert lst.runs == []
    assert lst.total == 0
    assert lst.has_more is False

    params = route.calls.last.request.url.params
    assert "tenant" not in params
    assert "status" not in params
    assert params["limit"] == "50"
    assert params["offset"] == "0"


# ─── get ──────────────────────────────────────────────────────────────────


async def test_get_returns_run_row() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/sandbox/runs/sbx_123").mock(
            return_value=httpx.Response(
                200,
                json=_run_row(status="failed", exit_code=137, duration_s=2.0),
            )
        )
        run_row = await sandbox.get("sbx_123")
    assert run_row.id == "sbx_123"
    assert run_row.status == "failed"
    assert run_row.exit_code == 137
    assert run_row.duration_s == 2.0


async def test_get_rejects_empty_id() -> None:
    with pytest.raises(ValueError):
        await sandbox.get("")


# ─── stop ─────────────────────────────────────────────────────────────────


async def test_stop_hits_delete_endpoint() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.delete(f"{BASE}/sandbox/runs/sbx_123").mock(
            return_value=httpx.Response(200, json={"stopped": True})
        )
        ok = await sandbox.stop("sbx_123")
    assert ok is True
    assert route.calls.last.request.method == "DELETE"


async def test_stop_rejects_empty_id() -> None:
    with pytest.raises(ValueError):
        await sandbox.stop("")


# ─── pool ─────────────────────────────────────────────────────────────────


async def test_pool_returns_stats_with_capabilities() -> None:
    payload = {
        "adapter": "docker",
        "warm": 3,
        "active": 1,
        "queued": 0,
        "total_runs_today": 42,
        "cpu_seconds_today": 318.4,
        "cost_usd_today": 0.51,
        "capabilities": _capabilities(),
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/sandbox/pool").mock(
            return_value=httpx.Response(200, json=payload)
        )
        stats = await sandbox.pool()
    assert stats.adapter == "docker"
    assert stats.warm == 3
    assert stats.active == 1
    assert stats.total_runs_today == 42
    assert stats.capabilities.max_timeout_s == 86400
    assert stats.capabilities.supports_gpu is False
    assert stats.capabilities.adapter == "docker"


# ─── namespace ────────────────────────────────────────────────────────────


async def test_suite_sandbox_namespace_matches_module() -> None:
    assert suite.sandbox is sandbox
    assert suite.sandbox.run is sandbox.run
    assert suite.sandbox.list is sandbox.list
    assert suite.sandbox.get is sandbox.get
    assert suite.sandbox.stop is sandbox.stop
    assert suite.sandbox.pool is sandbox.pool
