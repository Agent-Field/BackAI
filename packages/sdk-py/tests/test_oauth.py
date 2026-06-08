# SPDX-License-Identifier: Apache-2.0

"""Unit tests for the ``af_stack.oauth`` module."""

from __future__ import annotations

from typing import Any

import pytest

from af_stack import oauth, suite


RequestCall = dict[str, Any]


def patch_request_json(
    monkeypatch: pytest.MonkeyPatch,
    response: Any,
) -> list[RequestCall]:
    calls: list[RequestCall] = []

    async def fake_request_json(
        method: str,
        path: str,
        **kwargs: Any,
    ) -> Any:
        calls.append({"method": method, "path": path, **kwargs})
        return response

    monkeypatch.setattr(oauth._http, "request_json", fake_request_json)
    return calls


async def test_authorize_url_posts_scopes_and_return_to(monkeypatch: pytest.MonkeyPatch) -> None:
    calls = patch_request_json(
        monkeypatch,
        {
            "provider": "github",
            "authorization_url": "https://github.com/login/oauth/authorize?state=s",
            "redirect_uri": "http://localhost:8080/oauth/callback/github",
            "scopes": ["repo"],
        },
    )

    url = await oauth.authorize_url(
        "github",
        scopes=["repo"],
        return_to="http://localhost:3000/integrations",
    )

    assert url == "https://github.com/login/oauth/authorize?state=s"
    assert calls == [
        {
            "method": "POST",
            "path": "/oauth/github/authorize",
            "json": {
                "scopes": ["repo"],
                "return_to": "http://localhost:3000/integrations",
            },
        }
    ]


async def test_connected_parses_metadata_without_token_bytes(monkeypatch: pytest.MonkeyPatch) -> None:
    calls = patch_request_json(
        monkeypatch,
        {
            "connections": [
                {
                    "provider": "google",
                    "scopes": ["https://www.googleapis.com/auth/drive.readonly"],
                    "connected_at": "2026-06-07T10:00:00Z",
                    "expires_at": "2026-06-07T11:00:00Z",
                }
            ]
        },
    )

    conns = await oauth.connected()

    assert len(conns.connections) == 1
    assert conns.connections[0].provider == "google"
    assert calls == [{"method": "GET", "path": "/oauth/connections"}]


async def test_token_posts_internal_header_and_returns_access_token(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls = patch_request_json(
        monkeypatch,
        {
            "provider": "github",
            "user_id": "11111111-1111-1111-1111-111111111111",
            "access_token": "gho_secret",
            "scopes": ["repo"],
        },
    )

    token = await oauth.token("github", user_id="11111111-1111-1111-1111-111111111111")

    assert token == "gho_secret"
    assert calls == [
        {
            "method": "POST",
            "path": "/oauth/token",
            "json": {
                "provider": "github",
                "user_id": "11111111-1111-1111-1111-111111111111",
            },
            "headers": {"X-AF-Stack-Internal": "1"},
        }
    ]


async def test_disconnect_sends_provider_and_user_id(monkeypatch: pytest.MonkeyPatch) -> None:
    calls = patch_request_json(monkeypatch, {"disconnected": True})

    assert await oauth.disconnect("github", user_id="user-1") is True
    assert calls == [{"method": "DELETE", "path": "/oauth/github?user_id=user-1"}]


async def test_input_validation(monkeypatch: pytest.MonkeyPatch) -> None:
    patch_request_json(monkeypatch, {})

    with pytest.raises(ValueError):
        await oauth.authorize_url("")
    with pytest.raises(ValueError):
        await oauth.token("")
    with pytest.raises(ValueError):
        await oauth.disconnect("")


def test_suite_namespace_exposes_oauth_module() -> None:
    assert suite.oauth is oauth
