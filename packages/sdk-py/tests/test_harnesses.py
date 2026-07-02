# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for the ``af_stack.harnesses`` module.

Endpoint paths and JSON shapes mirror ``apps/dashboard/src/lib/api.ts``
(the canonical contract). The shared httpx client is reset between
tests so each test sees a clean client bound to a deterministic base
URL and API key.
"""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import harnesses, suite
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


def _row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "provider": "claude-code",
        "is_installed": True,
        "binary_path": "/usr/local/bin/claude",
        "version": "claude 1.2.3",
        "status": "ready",
        "last_error": None,
        "required_env": ["ANTHROPIC_API_KEY"],
        "install_doc_url": "https://docs.claude.com/claude-code",
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


# ─── list ─────────────────────────────────────────────────────────────────


async def test_list_returns_all_providers() -> None:
    payload = {
        "harnesses": [
            _row(provider="claude-code"),
            _row(
                provider="codex",
                status="needs_auth",
                required_env=["OPENAI_API_KEY"],
                binary_path="/usr/bin/codex",
            ),
            _row(
                provider="gemini",
                status="missing",
                is_installed=False,
                binary_path=None,
                version=None,
                required_env=["GEMINI_API_KEY"],
            ),
            _row(
                provider="opencode",
                status="ready",
                binary_path="/usr/bin/opencode",
                required_env=[],
            ),
        ]
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/harnesses").mock(return_value=httpx.Response(200, json=payload))
        result = await harnesses.list()
    assert len(result.harnesses) == 4
    assert [h.provider for h in result.harnesses] == [
        "claude-code",
        "codex",
        "gemini",
        "opencode",
    ]
    assert result.harnesses[0].status == "ready"
    assert result.harnesses[2].status == "missing"
    assert result.harnesses[2].binary_path is None


async def test_list_handles_empty_payload() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/harnesses").mock(
            return_value=httpx.Response(200, json={"harnesses": []})
        )
        result = await harnesses.list()
    assert result.harnesses == []


# ─── get ──────────────────────────────────────────────────────────────────


async def test_get_returns_single_harness() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/harnesses/claude-code").mock(
            return_value=httpx.Response(200, json=_row())
        )
        h = await harnesses.get("claude-code")
    assert h.provider == "claude-code"
    assert h.is_installed is True
    assert h.version == "claude 1.2.3"
    assert h.required_env == ["ANTHROPIC_API_KEY"]


async def test_get_rejects_unknown_provider() -> None:
    with pytest.raises(ValueError, match="must be one of"):
        await harnesses.get("nonexistent")


# ─── probe ────────────────────────────────────────────────────────────────


async def test_probe_posts_and_returns_fresh_row() -> None:
    payload = _row(status="needs_auth", last_error="required env not set: ANTHROPIC_API_KEY")
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/harnesses/claude-code/probe").mock(
            return_value=httpx.Response(200, json=payload)
        )
        h = await harnesses.probe("claude-code")
    assert h.status == "needs_auth"
    assert h.last_error == "required env not set: ANTHROPIC_API_KEY"
    assert route.calls.last.request.method == "POST"


async def test_probe_rejects_unknown_provider() -> None:
    with pytest.raises(ValueError, match="must be one of"):
        await harnesses.probe("bogus")


# ─── suite namespace ──────────────────────────────────────────────────────


async def test_suite_namespace_includes_harnesses() -> None:
    assert suite.harnesses is harnesses
    assert hasattr(suite.harnesses, "list")
    assert hasattr(suite.harnesses, "get")
    assert hasattr(suite.harnesses, "probe")
