# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for ``af_stack.worker`` — the pull-based remote worker.

Every runtime call is mocked with respx; no network + no runtime needed.
Heartbeats are disabled for most tests by setting a very large interval so
the background thread never fires during a fast handler.
"""

from __future__ import annotations

import json

import httpx
import pytest
import respx

from af_stack.worker import JobContext, PermanentError, Worker

BASE = "http://localhost:8080"
WPREFIX = f"{BASE}/api/v1/jobs/worker"


def make_worker(**kw: object) -> Worker:
    kw.setdefault("heartbeat_interval", 3600)
    kw.setdefault("poll_wait", 0)
    return Worker(BASE, "test-key", **kw)  # type: ignore[arg-type]


def _attempt(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "job_id": "7",
        "attempt": 1,
        "kind": "resize",
        "payload": {"x": 1},
        "tenant_id": "t_abc",
        "deadline": None,
        "lease_expires_at": None,
    }
    base.update(overrides)
    return base


def test_requires_base_url_and_key() -> None:
    with pytest.raises(ValueError):
        Worker("", "k")
    with pytest.raises(ValueError):
        Worker(BASE, "")


def test_run_requires_a_handler() -> None:
    with pytest.raises(RuntimeError):
        make_worker().run(install_signal_handlers=False)


def test_lease_once_sends_kinds_and_worker_id() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{WPREFIX}/lease").mock(
            return_value=httpx.Response(200, json={"job": _attempt()})
        )
        w = make_worker()

        @w.register("resize")
        def _h(payload, ctx):  # type: ignore[no-untyped-def]
            return {}

        got = w._lease_once()

    assert got is not None and got["job_id"] == "7"
    body = json.loads(route.calls.last.request.content)
    assert body["kinds"] == ["resize"]
    assert body["worker_id"] == w.worker_id
    assert body["lease_ttl_seconds"] == w.lease_ttl


def test_lease_once_returns_none_when_empty() -> None:
    with respx.mock as router:
        router.post(f"{WPREFIX}/lease").mock(return_value=httpx.Response(200, json={"job": None}))
        w = make_worker()

        @w.register("resize")
        def _h(payload, ctx):  # type: ignore[no-untyped-def]
            return {}

        assert w._lease_once() is None


def test_process_completes_with_handler_result() -> None:
    with respx.mock as router:
        complete = router.post(f"{WPREFIX}/complete").mock(
            return_value=httpx.Response(200, json={"ok": True})
        )
        w = make_worker()
        seen: dict[str, object] = {}

        @w.register("resize")
        def _h(payload, ctx: JobContext):  # type: ignore[no-untyped-def]
            seen["payload"] = payload
            seen["tenant"] = ctx.tenant_id
            seen["job_id"] = ctx.job_id
            return {"r": payload["x"] + 1}

        w._process(_attempt())

    assert seen["payload"] == {"x": 1}
    assert seen["tenant"] == "t_abc"
    body = json.loads(complete.calls.last.request.content)
    assert body["result"] == {"r": 2}
    assert body["worker_id"] == w.worker_id
    assert body["job_id"] == "7"


def test_process_fails_retryably_on_exception() -> None:
    with respx.mock as router:
        fail = router.post(f"{WPREFIX}/fail").mock(
            return_value=httpx.Response(200, json={"ok": True})
        )
        w = make_worker()

        @w.register("resize")
        def _h(payload, ctx):  # type: ignore[no-untyped-def]
            raise RuntimeError("boom")

        w._process(_attempt())

    body = json.loads(fail.calls.last.request.content)
    assert body["retryable"] is True
    assert body["error"] == "boom"


def test_process_fails_permanently_on_permanent_error() -> None:
    with respx.mock as router:
        fail = router.post(f"{WPREFIX}/fail").mock(
            return_value=httpx.Response(200, json={"ok": True})
        )
        w = make_worker()

        @w.register("resize")
        def _h(payload, ctx):  # type: ignore[no-untyped-def]
            raise PermanentError("nope")

        w._process(_attempt())

    body = json.loads(fail.calls.last.request.content)
    assert body["retryable"] is False
    assert body["error"] == "nope"


def test_process_skips_report_when_canceled() -> None:
    # No complete/fail route defined: if the worker tried to report, the
    # request would 404 through respx and raise — so a clean run proves it
    # skipped reporting.
    with respx.mock as router:
        router.post(f"{WPREFIX}/heartbeat").mock(
            return_value=httpx.Response(200, json={"canceled": True})
        )
        w = make_worker()

        @w.register("resize")
        def _h(payload, ctx: JobContext):  # type: ignore[no-untyped-def]
            # Simulate observing cancellation mid-handler.
            w._send_heartbeat(ctx)
            assert ctx.is_canceled()
            return {"ignored": True}

        w._process(_attempt())  # must not raise


def test_ctx_log_posts_structured_line() -> None:
    with respx.mock as router:
        logs = router.post(f"{WPREFIX}/logs").mock(
            return_value=httpx.Response(200, json={"accepted": 1})
        )
        w = make_worker()
        ctx = JobContext(worker=w, tenant_id="t", job_id="7", attempt=2, deadline=None)
        ctx.log("resizing", level="warn", url="http://x")

    body = json.loads(logs.calls.last.request.content)
    assert body["job_id"] == "7"
    assert body["attempt"] == 2
    line = body["lines"][0]
    assert line["message"] == "resizing"
    assert line["level"] == "warn"
    assert line["fields"] == {"url": "http://x"}


def test_heartbeat_surfaces_cancellation() -> None:
    with respx.mock as router:
        router.post(f"{WPREFIX}/heartbeat").mock(
            return_value=httpx.Response(200, json={"canceled": True, "lease_expires_at": None})
        )
        w = make_worker()
        ctx = JobContext(worker=w, tenant_id="t", job_id="7", attempt=1, deadline=None)
        assert w._send_heartbeat(ctx) is True
        assert ctx.is_canceled() is True


def test_run_drains_after_handler_requests_stop() -> None:
    with respx.mock as router:
        router.post(f"{WPREFIX}/lease").mock(
            side_effect=[
                httpx.Response(200, json={"job": _attempt()}),
                httpx.Response(200, json={"job": None}),
            ]
        )
        router.post(f"{WPREFIX}/complete").mock(return_value=httpx.Response(200, json={"ok": True}))
        w = make_worker()
        ran: list[int] = []

        @w.register("resize")
        def _h(payload, ctx):  # type: ignore[no-untyped-def]
            ran.append(1)
            w.stop()  # graceful drain after this job
            return {}

        w.run(install_signal_handlers=False)

    assert ran == [1]
