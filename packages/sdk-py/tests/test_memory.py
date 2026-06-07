"""HTTP-mocked tests for ``af_stack.memory``.

Mirrors the canonical contract in ``apps/dashboard/src/lib/api.ts``
(Phase 8 Memory section).
"""

from __future__ import annotations

import json

import httpx
import pytest
import respx

from af_stack import memory, suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


def _entry_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "scope": "tenant",
        "scope_id": "t-acme",
        "key": "user:42:prefs",
        "value": {"theme": "dark", "lang": "en"},
        "metadata": {"source": "settings-form"},
        "has_embedding": False,
        "created_at": "2026-06-06T12:00:00Z",
        "updated_at": "2026-06-06T12:00:00Z",
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


async def test_get_returns_entry_and_forwards_scope_filters() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/memory/get").mock(
            return_value=httpx.Response(200, json=_entry_row())
        )
        entry = await memory.get("tenant", "user:42:prefs", "t-acme")
    assert entry.scope == "tenant"
    assert entry.scope_id == "t-acme"
    assert entry.key == "user:42:prefs"
    assert entry.value == {"theme": "dark", "lang": "en"}
    assert entry.has_embedding is False
    params = route.calls.last.request.url.params
    assert params["scope"] == "tenant"
    assert params["key"] == "user:42:prefs"
    assert params["scope_id"] == "t-acme"


async def test_get_omits_scope_id_when_not_supplied() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/memory/get").mock(
            return_value=httpx.Response(200, json=_entry_row(scope="global", scope_id=None))
        )
        await memory.get("global", "feature_flag")
    params = route.calls.last.request.url.params
    assert params["scope"] == "global"
    assert params["key"] == "feature_flag"
    assert "scope_id" not in params


async def test_put_forwards_embed_metadata_and_scope_id() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.put(f"{BASE}/memory").mock(
            return_value=httpx.Response(
                200, json=_entry_row(has_embedding=True)
            )
        )
        entry = await memory.put(
            "tenant",
            "user:42:prefs",
            {"theme": "dark", "lang": "en"},
            scope_id="t-acme",
            metadata={"source": "settings-form"},
            embed=True,
        )
    assert entry.has_embedding is True
    payload = json.loads(route.calls.last.request.read().decode())
    assert payload["scope"] == "tenant"
    assert payload["scope_id"] == "t-acme"
    assert payload["key"] == "user:42:prefs"
    assert payload["value"] == {"theme": "dark", "lang": "en"}
    assert payload["metadata"] == {"source": "settings-form"}
    assert payload["embed"] is True


async def test_put_defaults_embed_false_and_omits_unset_optionals() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.put(f"{BASE}/memory").mock(
            return_value=httpx.Response(200, json=_entry_row())
        )
        await memory.put("global", "feature_flag", "enabled")
    payload = json.loads(route.calls.last.request.read().decode())
    assert payload["scope"] == "global"
    assert payload["key"] == "feature_flag"
    assert payload["value"] == "enabled"
    assert payload["embed"] is False
    assert "scope_id" not in payload
    assert "metadata" not in payload


async def test_delete_sends_query_params_and_returns_bool() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.delete(f"{BASE}/memory").mock(
            return_value=httpx.Response(200, json={"deleted": True})
        )
        ok = await memory.delete("tenant", "user:42:prefs", "t-acme")
    assert ok is True
    params = route.calls.last.request.url.params
    assert params["scope"] == "tenant"
    assert params["key"] == "user:42:prefs"
    assert params["scope_id"] == "t-acme"


async def test_list_forwards_filters_and_parses_paginated_response() -> None:
    body = {
        "entries": [
            _entry_row(key="user:1:prefs"),
            _entry_row(key="user:2:prefs"),
        ],
        "total": 12,
        "has_more": True,
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/memory").mock(
            return_value=httpx.Response(200, json=body)
        )
        result = await memory.list(
            scope="tenant",
            scope_id="t-acme",
            prefix="user:",
            limit=25,
            offset=50,
        )
    assert result.total == 12
    assert result.has_more is True
    assert [e.key for e in result.entries] == ["user:1:prefs", "user:2:prefs"]
    params = route.calls.last.request.url.params
    assert params["scope"] == "tenant"
    assert params["scope_id"] == "t-acme"
    assert params["prefix"] == "user:"
    assert params["limit"] == "25"
    assert params["offset"] == "50"


async def test_search_posts_query_and_returns_hits_in_similarity_order() -> None:
    body = {
        "hits": [
            {
                "entry": _entry_row(key="doc:alpha", has_embedding=True),
                "similarity": 0.92,
                "distance": 0.08,
            },
            {
                "entry": _entry_row(key="doc:beta", has_embedding=True),
                "similarity": 0.71,
                "distance": 0.29,
            },
        ],
        "duration_ms": 17,
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/memory/search").mock(
            return_value=httpx.Response(200, json=body)
        )
        result = await memory.search(
            "dark mode preferences",
            scope="tenant",
            scope_id="t-acme",
            top_k=5,
            threshold=0.5,
        )
    assert result.duration_ms == 17
    assert [h.entry.key for h in result.hits] == ["doc:alpha", "doc:beta"]
    # Similarity should be monotonically non-increasing — verifies order.
    sims = [h.similarity for h in result.hits]
    assert sims == sorted(sims, reverse=True)

    payload = json.loads(route.calls.last.request.read().decode())
    assert payload["query"] == "dark mode preferences"
    assert payload["scope"] == "tenant"
    assert payload["scope_id"] == "t-acme"
    assert payload["top_k"] == 5
    assert payload["threshold"] == 0.5


async def test_validation_errors_for_bad_inputs() -> None:
    with pytest.raises(ValueError):
        await memory.get("invalid-scope", "k")
    with pytest.raises(ValueError):
        await memory.get("tenant", "")
    with pytest.raises(ValueError):
        await memory.put("tenant", "", "v")
    with pytest.raises(ValueError):
        await memory.search("")
    with pytest.raises(ValueError):
        await memory.search("q", top_k=0)
    with pytest.raises(ValueError):
        await memory.search("q", top_k=999)
    with pytest.raises(ValueError):
        await memory.search("q", threshold=-0.1)
    with pytest.raises(ValueError):
        await memory.search("q", threshold=1.5)
    with pytest.raises(ValueError):
        await memory.list(limit=0)
    with pytest.raises(ValueError):
        await memory.list(offset=-1)


async def test_get_raises_on_structured_error() -> None:
    error_body = {
        "error": {
            "code": "MEMORY_NOT_FOUND",
            "message": "no such key",
            "request_id": "req_x",
        }
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/memory/get").mock(
            return_value=httpx.Response(404, json=error_body)
        )
        with pytest.raises(AFStackError) as exc:
            await memory.get("tenant", "missing", "t-acme")
    assert exc.value.code == "MEMORY_NOT_FOUND"
    assert exc.value.status_code == 404


async def test_suite_memory_namespace_matches_module() -> None:
    assert suite.memory is memory
    assert suite.memory.get is memory.get
    assert suite.memory.put is memory.put
    assert suite.memory.delete is memory.delete
    assert suite.memory.list is memory.list
    assert suite.memory.search is memory.search
