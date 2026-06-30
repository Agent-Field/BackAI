# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for the ``af_stack.agents`` module.

The shared httpx client is reset between tests so each test sees a clean
client bound to a deterministic base URL and API key.
"""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import agents, ctx, scope
from af_stack._http import AFStackError
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


@pytest.fixture(autouse=True)
async def reset_http_client(monkeypatch: pytest.MonkeyPatch) -> None:
    """Force a fresh shared client per test and clean it up afterwards."""
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "test-key")
    await http_close()
    yield
    await http_close()


async def test_call_sends_post_and_parses_result() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/agents/notable.summarize").mock(
            return_value=httpx.Response(
                200,
                json={
                    "execution_id": "exec_abc",
                    "status": "done",
                    "output": {"summary": "hello"},
                },
            )
        )
        result = await agents.call("notable.summarize", {"text": "long doc"}, timeout_s=5.0)
    assert result.execution_id == "exec_abc"
    assert result.output == {"summary": "hello"}
    assert result.status == "done"

    request = route.calls.last.request
    body = request.read().decode()
    assert "long doc" in body
    assert request.headers["authorization"] == "Bearer test-key"
    assert request.headers["accept"] == "application/json"


async def test_call_propagates_ctx_headers() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/agents/ns.func").mock(
            return_value=httpx.Response(200, json={"execution_id": "exec_1"})
        )
        with scope(tenant_id="t_xyz", user_id="u_pqr", request_id="req_fixed"):
            await agents.call("ns.func", {"x": 1})
            # ``ctx`` proxy reads from contextvars and should still hold values
            assert ctx.tenant_id == "t_xyz"

    headers = route.calls.last.request.headers
    assert headers["x-af-stack-tenant-id"] == "t_xyz"
    assert headers["x-af-stack-user-id"] == "u_pqr"
    assert headers["x-request-id"] == "req_fixed"


async def test_call_async_returns_handle() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/agents/async/ns.func").mock(
            return_value=httpx.Response(
                202,
                json={"execution_id": "exec_async", "status": "queued"},
            )
        )
        handle = await agents.call_async("ns.func", {"x": 1}, webhook_url="https://example.com/cb")
    assert handle.execution_id == "exec_async"
    assert handle.id == "exec_async"
    assert handle.status == "queued"


async def test_status_parses_response() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/executions/exec_1").mock(
            return_value=httpx.Response(
                200,
                json={
                    "execution_id": "exec_1",
                    "status": "running",
                    "started_at": "2026-06-06T12:00:00Z",
                },
            )
        )
        result = await agents.status("exec_1")
    assert result.execution_id == "exec_1"
    assert result.status == "running"
    assert result.started_at == "2026-06-06T12:00:00Z"


async def test_cancel_issues_delete_with_reason() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.delete(f"{BASE}/executions/exec_1").mock(return_value=httpx.Response(204))
        await agents.cancel("exec_1", reason="user-request")
    assert route.calls.last.request.url.params["reason"] == "user-request"


async def test_approve_posts_payload() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/executions/exec_1/approve").mock(
            return_value=httpx.Response(204)
        )
        await agents.approve("exec_1", by_user_id="u_1", note="lgtm")
    body = route.calls.last.request.read().decode()
    assert "u_1" in body
    assert "lgtm" in body


async def test_deny_posts_payload() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/executions/exec_1/deny").mock(return_value=httpx.Response(204))
        await agents.deny("exec_1", reason="policy", by_user_id="u_1")
    body = route.calls.last.request.read().decode()
    assert "policy" in body


async def test_pending_approvals_parses_list() -> None:
    payload = [
        {"execution_id": "exec_1", "agent": "ns.func", "reason": "needs human"},
        {"execution_id": "exec_2", "agent": "ns.func2"},
    ]
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/approvals/pending").mock(return_value=httpx.Response(200, json=payload))
        rows = await agents.pending_approvals()
    assert len(rows) == 2
    assert rows[0].execution_id == "exec_1"
    assert rows[0].reason == "needs human"
    assert rows[1].execution_id == "exec_2"


async def test_pending_approvals_handles_envelope() -> None:
    payload = {"items": [{"execution_id": "exec_a", "agent": "ns.f"}]}
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/approvals/pending").mock(return_value=httpx.Response(200, json=payload))
        rows = await agents.pending_approvals()
    assert [r.execution_id for r in rows] == ["exec_a"]


async def test_call_raises_structured_error() -> None:
    error_body = {
        "error": {
            "code": "VALIDATION_FAILED",
            "message": "field 'input.x' is required",
            "request_id": "req_remote",
            "details": {"field": "input.x"},
        }
    }
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/agents/ns.func").mock(
            return_value=httpx.Response(422, json=error_body)
        )
        with pytest.raises(AFStackError) as exc_info:
            await agents.call("ns.func", {})
    err = exc_info.value
    assert err.code == "VALIDATION_FAILED"
    assert err.status_code == 422
    assert err.request_id == "req_remote"
    assert err.details == {"field": "input.x"}


@pytest.mark.parametrize("bad_name", ["", "no-dot", "."])
async def test_call_rejects_malformed_agent_name(bad_name: str) -> None:
    with pytest.raises(ValueError):
        await agents.call(bad_name, {})


# TODO: end-to-end SSE streaming test. respx does not model SSE chunked
# responses out of the box, so we cover the parser in unit tests against
# ``_http.stream_sse`` once a streaming fixture is available.
