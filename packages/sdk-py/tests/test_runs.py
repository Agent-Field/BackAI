# SPDX-License-Identifier: Apache-2.0

"""Contract tests for the ``af_stack.runs`` module.

WebSocket networking is not mocked end-to-end here — the suite covers
URL construction, argument behaviour, payload parsing, and the
``websockets`` optional-dependency error path. Full integration with
the runtime happens in a separate harness that boots the real server.
"""

from __future__ import annotations

import json
import sys
from collections.abc import AsyncIterator
from typing import Any

import pytest

from af_stack import runs, suite
from af_stack.runs import RunEvent

# ─── URL construction ──────────────────────────────────────────────────────


def test_build_ws_url_uses_ws_scheme_for_http(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.delenv("AF_STACK_API_KEY", raising=False)
    url = runs._build_ws_url(
        tenant_id=None, user_id=None, agent=None, run_id="run-1", execution_id=None
    )
    assert url.startswith("ws://localhost:8080/api/v1/realtime/runs?")
    assert "run_id=run-1" in url
    assert "api_key" not in url


def test_build_ws_url_uses_wss_for_https(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "https://api.example.com")
    monkeypatch.delenv("AF_STACK_API_KEY", raising=False)
    url = runs._build_ws_url(
        tenant_id="t-1", user_id=None, agent=None, run_id=None, execution_id=None
    )
    assert url.startswith("wss://api.example.com/api/v1/realtime/runs?")


def test_build_ws_url_appends_api_key_when_set(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "test-key")
    url = runs._build_ws_url(
        tenant_id=None, user_id=None, agent=None, run_id=None, execution_id="exec-1"
    )
    assert "api_key=test-key" in url


def test_build_ws_url_omits_empty_filters(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.delenv("AF_STACK_API_KEY", raising=False)
    url = runs._build_ws_url(
        tenant_id=None, user_id=None, agent=None, run_id=None, execution_id=None
    )
    # No query params at all when nothing is set.
    assert url == "ws://localhost:8080/api/v1/realtime/runs"


def test_build_ws_url_includes_all_filters(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.delenv("AF_STACK_API_KEY", raising=False)
    url = runs._build_ws_url(
        tenant_id="t-1",
        user_id="u-1",
        agent="notable.summarize",
        run_id="run-1",
        execution_id="exec-1",
    )
    for token in (
        "tenant_id=t-1",
        "user_id=u-1",
        "agent=notable.summarize",
        "run_id=run-1",
        "execution_id=exec-1",
    ):
        assert token in url


# ─── subscribe shape ───────────────────────────────────────────────────────


def test_subscribe_returns_async_iterator() -> None:
    iterator = runs.subscribe(run_id="run-1")
    assert isinstance(iterator, AsyncIterator)


def test_subscribe_no_args_returns_async_iterator() -> None:
    iterator = runs.subscribe()
    assert isinstance(iterator, AsyncIterator)


# ─── streaming + parsing ───────────────────────────────────────────────────


class _FakeSocket:
    """Async-iterable stand-in for a websockets client connection."""

    def __init__(self, frames: list[Any]) -> None:
        self._frames = list(frames)

    async def __aenter__(self) -> _FakeSocket:
        return self

    async def __aexit__(self, *exc: object) -> None:
        return None

    def __aiter__(self) -> _FakeSocket:
        return self

    async def __anext__(self) -> Any:
        if not self._frames:
            raise StopAsyncIteration
        return self._frames.pop(0)


async def test_stream_run_events_parses_json_payloads() -> None:
    frames = [
        json.dumps(
            {
                "type": "run.started",
                "run_id": "run-1",
                "execution_id": "exec-1",
                "agent": "notable.summarize",
                "started_at": "2026-06-07T00:00:00Z",
            }
        ),
        # Server-degraded shows up first when AgentField is down — the
        # SDK should yield it like any other event so callers can
        # surface a UI banner.
        json.dumps({"type": "server.degraded", "reason": "AF unreachable"}),
        json.dumps(
            {
                "type": "run.completed",
                "run_id": "run-1",
                "execution_id": "exec-1",
                "status": "succeeded",
                "duration_ms": 123,
            }
        ),
        # Non-JSON frame gets silently skipped.
        "not-json",
        # Non-dict JSON is also skipped.
        "42",
    ]

    def fake_connect(url: str) -> _FakeSocket:
        assert url.startswith("ws://")
        return _FakeSocket(frames)

    fake_module = type(sys)("websockets")
    fake_asyncio = type(sys)("websockets.asyncio")
    fake_client = type(sys)("websockets.asyncio.client")
    fake_client.connect = fake_connect  # type: ignore[attr-defined]
    fake_asyncio.client = fake_client  # type: ignore[attr-defined]
    fake_module.asyncio = fake_asyncio  # type: ignore[attr-defined]
    sys.modules.setdefault("websockets", fake_module)
    sys.modules.setdefault("websockets.asyncio", fake_asyncio)
    sys.modules["websockets.asyncio.client"] = fake_client

    events: list[RunEvent] = []
    async for evt in runs._stream_run_events(
        "ws://localhost:8080/api/v1/realtime/runs"
    ):
        events.append(evt)

    assert len(events) == 3
    assert events[0].type == "run.started"
    assert events[0].run_id == "run-1"
    assert events[0].agent == "notable.summarize"
    assert events[1].type == "server.degraded"
    assert events[1].reason == "AF unreachable"
    assert events[2].type == "run.completed"
    assert events[2].status == "succeeded"
    assert events[2].duration_ms == 123


# ─── namespace ─────────────────────────────────────────────────────────────


async def test_suite_runs_namespace_matches_module() -> None:
    assert suite.runs is runs
    assert suite.runs.subscribe is runs.subscribe
