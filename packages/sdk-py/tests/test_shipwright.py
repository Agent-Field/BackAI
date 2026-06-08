# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import shipwright, suite
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


def _task_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "task_123",
        "tenant_id": "tenant_1",
        "user_id": "user_1",
        "title": "Add export",
        "description": "Add CSV export",
        "repo_url": "https://github.com/acme/app",
        "status": "running",
        "run_id": "exec_123",
        "created_at": "2026-06-07T12:00:00Z",
        "updated_at": "2026-06-07T12:00:01Z",
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


async def test_create_posts_payload_and_parses_response() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/shipwright/tasks").mock(
            return_value=httpx.Response(
                202,
                json={
                    "task": _task_row(),
                    "agent_call": "shipwright.build",
                    "agentfield_url": "http://localhost:8081",
                    "details_url": "http://localhost:8081/agent-api/executions/exec_123/details",
                },
            )
        )
        result = await shipwright.create(
            title="Add export",
            description="Add CSV export",
            repo_url="https://github.com/acme/app",
            harness_provider="codex",
            model="openrouter/google/gemini-2.5-flash",
        )
    assert result.task.run_id == "exec_123"
    assert result.agent_call == "shipwright.build"
    body = route.calls.last.request.read().decode()
    assert "repo_url" in body
    assert "harness_provider" in body


async def test_list_sends_filters() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/shipwright/tasks").mock(
            return_value=httpx.Response(
                200,
                json={"tasks": [_task_row()], "total": 1, "has_more": False},
            )
        )
        result = await shipwright.list(status="running", limit=10, offset=5)
    assert result.tasks[0].tenant_id == "tenant_1"
    params = route.calls.last.request.url.params
    assert params["status"] == "running"
    assert params["limit"] == "10"
    assert params["offset"] == "5"


async def test_get_and_complete_parse_patches() -> None:
    patch = {
        "task_id": "task_123",
        "ref": "refs/heads/shipwright/task_123",
        "summary": "Done",
        "diff_url": "https://github.com/acme/app/pull/1",
        "created_at": "2026-06-07T12:10:00Z",
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/shipwright/tasks/task_123").mock(
            return_value=httpx.Response(
                200, json={"task": _task_row(status="succeeded"), "patches": [patch]}
            )
        )
        got = await shipwright.get("task_123")
    assert got.patches[0].diff_url == "https://github.com/acme/app/pull/1"

    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/shipwright/tasks/task_123/complete").mock(
            return_value=httpx.Response(
                200, json={"task": _task_row(status="succeeded"), "patches": [patch]}
            )
        )
        done = await shipwright.complete(
            "task_123",
            status="succeeded",
            ref="refs/heads/shipwright/task_123",
            summary="Done",
        )
    assert done.task.status == "succeeded"
    assert "refs/heads/shipwright/task_123" in route.calls.last.request.read().decode()


async def test_create_rejects_empty_title() -> None:
    with pytest.raises(ValueError):
        await shipwright.create(
            title="",
            description="Add CSV export",
            repo_url="https://github.com/acme/app",
        )


async def test_suite_shipwright_namespace_matches_module() -> None:
    assert suite.shipwright is shipwright
    assert suite.shipwright.create is shipwright.create
