# SPDX-License-Identifier: Apache-2.0

"""Contract tests for the ``af_stack.realtime`` module.

WebSocket networking is not mocked end-to-end here — we cover URL
construction, argument validation, payload parsing, and the
``websockets`` optional-dependency error path. Full integration with the
runtime happens in a separate harness that boots the real server.
"""

from __future__ import annotations

import json
import sys
from collections.abc import AsyncIterator
from typing import Any

import pytest

from af_stack import realtime, suite
from af_stack.realtime import RealtimeEvent

# ─── URL construction ──────────────────────────────────────────────────────


def test_build_ws_url_uses_ws_scheme_for_http(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.delenv("AF_STACK_API_KEY", raising=False)
    url = realtime._build_ws_url("notes", {})
    assert url.startswith("ws://localhost:8080/api/v1/realtime?")
    assert "table=notes" in url
    assert "api_key" not in url
    assert "filter" not in url


def test_build_ws_url_uses_wss_for_https(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "https://api.example.com")
    monkeypatch.delenv("AF_STACK_API_KEY", raising=False)
    url = realtime._build_ws_url("notes", {})
    assert url.startswith("wss://api.example.com/api/v1/realtime?")


def test_build_ws_url_appends_api_key_when_set(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "test-key")
    url = realtime._build_ws_url("notes", {})
    assert "api_key=test-key" in url


def test_build_ws_url_serialises_filter_as_json(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.delenv("AF_STACK_API_KEY", raising=False)
    url = realtime._build_ws_url("notes", {"author_id": 42, "tag": "pinned"})
    # filter value travels url-encoded; round-trip via params lookup is
    # easier than string-matching the encoded JSON.
    from urllib.parse import parse_qs, urlsplit

    qs = parse_qs(urlsplit(url).query)
    assert qs["filter"]
    parsed = json.loads(qs["filter"][0])
    assert parsed == {"author_id": 42, "tag": "pinned"}


# ─── subscribe validation ──────────────────────────────────────────────────


def test_subscribe_rejects_empty_table() -> None:
    with pytest.raises(ValueError):
        realtime.subscribe("")
    with pytest.raises(ValueError):
        realtime.subscribe("   ")


def test_subscribe_returns_async_iterator() -> None:
    # Smoke test: the call itself is synchronous and returns an iterator
    # that hasn't opened the socket yet. Just confirm the shape.
    iterator = realtime.subscribe("notes")
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


async def test_stream_events_parses_json_payloads() -> None:
    frames = [
        json.dumps(
            {
                "table": "notes",
                "op": "insert",
                "tenant_id": "t_default",
                "record": {"id": 1, "title": "hi"},
            }
        ),
        # Non-JSON frame gets silently skipped, not an error — the runtime
        # may emit keepalives or control messages.
        "not-json",
        # Non-dict JSON is also skipped.
        "42",
        # Binary frame decoded as UTF-8.
        json.dumps({"table": "notes", "op": "update"}).encode("utf-8"),
    ]

    def fake_connect(url: str) -> _FakeSocket:
        assert url.startswith("ws://")
        return _FakeSocket(frames)

    events: list[RealtimeEvent] = []
    # Inject fake websockets module so the lazy import in _stream_events
    # picks it up without us needing real network.
    fake_module = type(sys)("websockets")
    fake_asyncio = type(sys)("websockets.asyncio")
    fake_client = type(sys)("websockets.asyncio.client")
    fake_client.connect = fake_connect  # type: ignore[attr-defined]
    fake_asyncio.client = fake_client  # type: ignore[attr-defined]
    fake_module.asyncio = fake_asyncio  # type: ignore[attr-defined]
    sys.modules.setdefault("websockets", fake_module)
    sys.modules.setdefault("websockets.asyncio", fake_asyncio)
    sys.modules["websockets.asyncio.client"] = fake_client

    async for evt in realtime._stream_events("ws://localhost:8080/api/v1/realtime"):
        events.append(evt)

    assert len(events) == 2
    assert events[0].table == "notes"
    assert events[0].op == "insert"
    assert events[0].record == {"id": 1, "title": "hi"}
    assert events[1].op == "update"


# ─── namespace ─────────────────────────────────────────────────────────────


async def test_suite_realtime_namespace_matches_module() -> None:
    assert suite.realtime is realtime
    assert suite.realtime.subscribe is realtime.subscribe
