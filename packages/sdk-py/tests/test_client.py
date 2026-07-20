# SPDX-License-Identifier: Apache-2.0

"""Behaviour tests for the explicit ``BackAI`` client.

Validation contract:
* An explicit client targets its own base_url / api_key (not env), and its
  namespace methods reach that runtime with the client's Bearer key.
* Transient failures (429/5xx) on safe (GET) methods retry up to max_retries,
  then succeed or raise the structured error.
* Non-idempotent mutations (POST) do NOT retry by default …
* … but DO retry when an ``idempotency_key`` is supplied, and that key is sent
  as the ``Idempotency-Key`` header.
* The runtime-version probe fires at most once and warns on a major mismatch,
  while tolerating a 404.
* Existing env-configured ``suite``/module calls are unaffected (no retries).
"""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import BackAI, agents
from af_stack._http import AFStackError
from af_stack._http import close as http_close


@pytest.fixture(autouse=True)
async def _zero_backoff(monkeypatch: pytest.MonkeyPatch) -> None:
    """Remove real sleeps so retry tests run instantly."""
    monkeypatch.setattr("af_stack._http._retry_delay", lambda response, attempt: 0.0)
    # Isolate from any ambient env so the default transport is deterministic.
    monkeypatch.delenv("AF_STACK_URL", raising=False)
    monkeypatch.delenv("AF_STACK_API_KEY", raising=False)
    await http_close()
    yield
    await http_close()


async def test_client_targets_its_own_base_url_and_key() -> None:
    client = BackAI(
        base_url="http://example.test",
        api_key="sk-live",
        check_runtime_version=False,
    )
    with respx.mock(assert_all_called=True) as router:
        route = router.post("http://example.test/api/v1/agents/ns.fn").mock(
            return_value=httpx.Response(200, json={"execution_id": "e1"})
        )
        result = await client.agents.call("ns.fn", {"x": 1})
    assert result.execution_id == "e1"
    req = route.calls.last.request
    assert req.headers["authorization"] == "Bearer sk-live"
    await client.close()


async def test_get_retries_transient_failures() -> None:
    client = BackAI(
        base_url="http://retry.test",
        max_retries=2,
        check_runtime_version=False,
    )
    with respx.mock(assert_all_called=True) as router:
        route = router.get("http://retry.test/api/v1/jobs/j1").mock(
            side_effect=[
                httpx.Response(503),
                httpx.Response(503),
                httpx.Response(
                    200,
                    json={
                        "id": "j1",
                        "name": "n",
                        "state": "completed",
                        "attempt": 1,
                        "max_attempts": 3,
                        "scheduled_at": "2026-01-01T00:00:00Z",
                        "enqueued_at": "2026-01-01T00:00:00Z",
                    },
                ),
            ]
        )
        job = await client.jobs.get("j1")
    assert job.id == "j1"
    assert route.call_count == 3
    await client.close()


async def test_get_exhausts_retries_then_raises() -> None:
    client = BackAI(
        base_url="http://exhaust.test",
        max_retries=1,
        check_runtime_version=False,
    )
    with respx.mock(assert_all_called=True) as router:
        route = router.get("http://exhaust.test/api/v1/jobs/j2").mock(
            return_value=httpx.Response(503, json={"error": {"code": "UPSTREAM"}})
        )
        with pytest.raises(AFStackError) as excinfo:
            await client.jobs.get("j2")
    assert excinfo.value.status == 503
    assert route.call_count == 2  # 1 initial + 1 retry
    await client.close()


async def test_mutation_does_not_retry_without_idempotency_key() -> None:
    client = BackAI(
        base_url="http://mut.test",
        max_retries=3,
        check_runtime_version=False,
    )
    with respx.mock(assert_all_called=True) as router:
        route = router.post("http://mut.test/api/v1/agents/ns.fn").mock(
            return_value=httpx.Response(503, json={"error": {"code": "BOOM"}})
        )
        with pytest.raises(AFStackError):
            await client.agents.call("ns.fn", {"x": 1})
    assert route.call_count == 1  # no retry for a bare mutation
    await client.close()


async def test_mutation_retries_with_idempotency_key_and_sends_header() -> None:
    client = BackAI(
        base_url="http://idem.test",
        max_retries=2,
        check_runtime_version=False,
    )
    with respx.mock(assert_all_called=True) as router:
        route = router.post("http://idem.test/api/v1/agents/ns.fn").mock(
            side_effect=[
                httpx.Response(503),
                httpx.Response(200, json={"execution_id": "e9"}),
            ]
        )
        result = await client.agents.call("ns.fn", {"x": 1}, idempotency_key="key-123")
    assert result.execution_id == "e9"
    assert route.call_count == 2
    assert route.calls.last.request.headers["idempotency-key"] == "key-123"
    await client.close()


async def test_version_probe_fires_once_and_warns_on_major_mismatch() -> None:
    client = BackAI(base_url="http://ver.test", check_runtime_version=True)
    with respx.mock(assert_all_called=True) as router:
        version_route = router.get("http://ver.test/api/v1/version").mock(
            return_value=httpx.Response(200, json={"version": "2.0.0"})
        )
        router.get("http://ver.test/api/v1/jobs/j1").mock(
            return_value=httpx.Response(
                200,
                json={
                    "id": "j1",
                    "name": "n",
                    "state": "completed",
                    "attempt": 1,
                    "max_attempts": 3,
                    "scheduled_at": "2026-01-01T00:00:00Z",
                    "enqueued_at": "2026-01-01T00:00:00Z",
                },
            )
        )
        with pytest.warns(RuntimeWarning, match="upgrade the SDK"):
            await client.jobs.get("j1")
        # Second call must NOT re-probe.
        await client.jobs.get("j1")
    assert version_route.call_count == 1
    await client.close()


async def test_version_probe_tolerates_404() -> None:
    client = BackAI(base_url="http://v404.test", check_runtime_version=True)
    with respx.mock(assert_all_called=True) as router:
        router.get("http://v404.test/api/v1/version").mock(return_value=httpx.Response(404))
        router.get("http://v404.test/api/v1/jobs/j1").mock(
            return_value=httpx.Response(
                200,
                json={
                    "id": "j1",
                    "name": "n",
                    "state": "completed",
                    "attempt": 1,
                    "max_attempts": 3,
                    "scheduled_at": "2026-01-01T00:00:00Z",
                    "enqueued_at": "2026-01-01T00:00:00Z",
                },
            )
        )
        import warnings

        with warnings.catch_warnings():
            warnings.simplefilter("error")  # any warning would fail the test
            await client.jobs.get("j1")
    await client.close()


async def test_env_default_path_does_not_retry(monkeypatch: pytest.MonkeyPatch) -> None:
    """The env-configured module path preserves legacy no-retry behaviour."""
    monkeypatch.setenv("AF_STACK_URL", "http://legacy.test")
    monkeypatch.setenv("AF_STACK_API_KEY", "k")
    await http_close()
    with respx.mock(assert_all_called=True) as router:
        route = router.get("http://legacy.test/api/v1/jobs/j3").mock(
            return_value=httpx.Response(503, json={"error": {"code": "X"}})
        )
        with pytest.raises(AFStackError):
            from af_stack import jobs

            await jobs.get("j3")
    assert route.call_count == 1
    await http_close()


def test_backai_exposes_config() -> None:
    client = BackAI(base_url="http://cfg.test/", max_retries=5, check_runtime_version=False)
    assert client.base_url == "http://cfg.test"
    assert client.max_retries == 5
    # agents.call remains usable as a bound coroutine function
    assert callable(client.agents.call)
    assert agents.call is not client.agents.call
