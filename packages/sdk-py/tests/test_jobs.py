# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for the ``af_stack.jobs`` module.

The shared httpx client is reset between tests so each test sees a clean
client bound to a deterministic base URL and API key. Endpoint paths and
JSON shapes mirror ``apps/dashboard/src/lib/api.ts`` (the canonical
contract).
"""

from __future__ import annotations

from datetime import datetime, timezone

import httpx
import pytest
import respx

from af_stack import jobs, suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


def _job_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "job_123",
        "name": "send-welcome-email",
        "args": {"to": "user@example.com"},
        "state": "available",
        "tenant_id": "t_xyz",
        "attempt": 0,
        "max_attempts": 5,
        "scheduled_at": "2026-06-06T12:00:00Z",
        "enqueued_at": "2026-06-06T12:00:00Z",
        "attempted_at": None,
        "finalized_at": None,
        "errors": None,
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


async def test_enqueue_posts_payload_and_parses_job() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/jobs").mock(
            return_value=httpx.Response(200, json=_job_row())
        )
        job = await jobs.enqueue(
            "send-welcome-email",
            {"to": "user@example.com"},
            max_attempts=5,
        )
    assert job.id == "job_123"
    assert job.name == "send-welcome-email"
    assert job.state == "available"
    body = route.calls.last.request.read().decode()
    assert "send-welcome-email" in body
    assert "user@example.com" in body
    assert "max_attempts" in body


async def test_enqueue_serialises_scheduled_at() -> None:
    when = datetime(2026, 6, 6, 12, 0, tzinfo=timezone.utc)
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/jobs").mock(
            return_value=httpx.Response(200, json=_job_row(state="scheduled"))
        )
        job = await jobs.enqueue("nightly-report", scheduled_at=when)
    assert job.state == "scheduled"
    body = route.calls.last.request.read().decode()
    assert "scheduled_at" in body
    assert "2026-06-06T12:00:00" in body


async def test_enqueue_serialises_naive_datetime_as_utc() -> None:
    when = datetime(2026, 6, 6, 12, 0)
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/jobs").mock(
            return_value=httpx.Response(200, json=_job_row(state="scheduled"))
        )
        await jobs.enqueue("nightly-report", scheduled_at=when)
    body = route.calls.last.request.read().decode()
    # Naive datetimes serialised with a `Z` suffix.
    assert "2026-06-06T12:00:00Z" in body


async def test_enqueue_rejects_empty_name() -> None:
    with pytest.raises(ValueError):
        await jobs.enqueue("")


async def test_enqueue_rejects_out_of_range_max_attempts() -> None:
    with pytest.raises(ValueError):
        await jobs.enqueue("x", max_attempts=0)
    with pytest.raises(ValueError):
        await jobs.enqueue("x", max_attempts=999)


async def test_get_returns_job() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/jobs/job_123").mock(
            return_value=httpx.Response(
                200,
                json=_job_row(state="running", attempt=1),
            )
        )
        job = await jobs.get("job_123")
    assert job.id == "job_123"
    assert job.state == "running"
    assert job.attempt == 1


async def test_retry_posts_to_retry_endpoint() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/jobs/job_123/retry").mock(
            return_value=httpx.Response(
                200, json=_job_row(state="available", attempt=2)
            )
        )
        job = await jobs.retry("job_123")
    assert job.state == "available"
    assert job.attempt == 2
    assert route.calls.last.request.method == "POST"


async def test_list_sends_filters_as_query_params() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/jobs").mock(
            return_value=httpx.Response(
                200,
                json={
                    "jobs": [_job_row(id="job_1"), _job_row(id="job_2")],
                    "total": 42,
                    "has_more": True,
                },
            )
        )
        lst = await jobs.list(
            name="send-welcome-email",
            state="running",
            tenant="t_xyz",
            limit=10,
            offset=0,
        )
    assert lst.total == 42
    assert lst.has_more is True
    assert [j.id for j in lst.jobs] == ["job_1", "job_2"]

    params = route.calls.last.request.url.params
    assert params["name"] == "send-welcome-email"
    assert params["state"] == "running"
    assert params["tenant"] == "t_xyz"
    assert params["limit"] == "10"
    assert params["offset"] == "0"


async def test_list_omits_unset_filters() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/jobs").mock(
            return_value=httpx.Response(
                200, json={"jobs": [], "total": 0, "has_more": False}
            )
        )
        await jobs.list()
    params = route.calls.last.request.url.params
    assert "name" not in params
    assert "state" not in params
    assert "tenant" not in params
    assert params["limit"] == "50"
    assert params["offset"] == "0"


async def test_enqueue_raises_on_structured_error() -> None:
    error_body = {
        "error": {
            "code": "JOB_NOT_REGISTERED",
            "message": "no such job",
            "request_id": "req_x",
        }
    }
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/jobs").mock(
            return_value=httpx.Response(404, json=error_body)
        )
        with pytest.raises(AFStackError) as exc:
            await jobs.enqueue("unknown-job")
    assert exc.value.code == "JOB_NOT_REGISTERED"
    assert exc.value.status_code == 404


async def test_suite_jobs_namespace_matches_module() -> None:
    assert suite.jobs is jobs
    assert suite.jobs.enqueue is jobs.enqueue