# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked contract tests for the ``af_stack.search`` module.

Verifies request shape (URL, method, JSON body) and response parsing
against the wire contract defined in
``services/runtime/internal/server/search.go``.
"""

from __future__ import annotations

import json

import httpx
import pytest
import respx

from af_stack import search, suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close
from af_stack.search import SearchDocument, SearchResult

BASE = "http://localhost:8080/api/v1"


def _doc(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "tenant_id": "t_default",
        "namespace": "default",
        "key": "notes/1",
        "title": "first",
        "body": "hello world",
        "metadata": {"author": "alice"},
        "has_embedding": True,
        "created_at": "2026-06-01T00:00:00Z",
        "updated_at": "2026-06-01T00:00:00Z",
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


# ─── search ────────────────────────────────────────────────────────────────


async def test_search_posts_query_with_defaults() -> None:
    payload = {
        "hits": [
            {
                "document": _doc(),
                "score": 0.91,
                "fts_rank": 0.7,
                "similarity": 0.85,
            }
        ],
        "mode": "hybrid",
        "duration_ms": 42,
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/search").mock(
            return_value=httpx.Response(200, json=payload)
        )
        result = await search.search("hello")

    assert isinstance(result, SearchResult)
    assert result.mode == "hybrid"
    assert result.duration_ms == 42
    assert len(result.hits) == 1
    hit = result.hits[0]
    assert isinstance(hit.document, SearchDocument)
    assert hit.document.key == "notes/1"
    assert hit.score == 0.91

    body = json.loads(route.calls.last.request.read().decode())
    assert body["query"] == "hello"
    assert body["mode"] == "hybrid"
    assert body["limit"] == 10
    assert body["offset"] == 0
    # No namespace / metadata_filter when not provided.
    assert "namespace" not in body
    assert "metadata_filter" not in body


async def test_search_posts_optional_namespace_and_filter() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/search").mock(
            return_value=httpx.Response(
                200, json={"hits": [], "mode": "fts", "duration_ms": 5}
            )
        )
        await search.search(
            "hello",
            mode="fts",
            namespace="reports",
            metadata_filter={"author": "alice"},
            limit=25,
            offset=50,
        )
    body = json.loads(route.calls.last.request.read().decode())
    assert body["mode"] == "fts"
    assert body["namespace"] == "reports"
    assert body["metadata_filter"] == {"author": "alice"}
    assert body["limit"] == 25
    assert body["offset"] == 50


async def test_search_validates_inputs() -> None:
    with pytest.raises(ValueError):
        await search.search("")
    with pytest.raises(ValueError):
        await search.search("ok", mode="bogus")  # type: ignore[arg-type]
    with pytest.raises(ValueError):
        await search.search("ok", limit=0)
    with pytest.raises(ValueError):
        await search.search("ok", limit=200)
    with pytest.raises(ValueError):
        await search.search("ok", offset=-1)


async def test_search_raises_on_structured_error() -> None:
    error_body = {
        "error": {
            "code": "EMBEDDER_NOT_CONFIGURED",
            "message": "vector search requires an embedder",
            "request_id": "req_x",
        }
    }
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/search").mock(
            return_value=httpx.Response(503, json=error_body)
        )
        with pytest.raises(AFStackError) as exc:
            await search.search("vec", mode="vector")
    assert exc.value.code == "EMBEDDER_NOT_CONFIGURED"
    assert exc.value.status_code == 503


# ─── upsert ────────────────────────────────────────────────────────────────


async def test_upsert_puts_document() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.put(f"{BASE}/search/documents").mock(
            return_value=httpx.Response(200, json=_doc())
        )
        doc = await search.upsert(
            "notes/1",
            namespace="default",
            title="first",
            body="hello world",
            metadata={"author": "alice"},
            embed=True,
        )
    assert isinstance(doc, SearchDocument)
    assert doc.key == "notes/1"
    body = json.loads(route.calls.last.request.read().decode())
    assert body["key"] == "notes/1"
    assert body["title"] == "first"
    assert body["body"] == "hello world"
    assert body["metadata"] == {"author": "alice"}
    assert body["embed"] is True
    assert body["namespace"] == "default"


async def test_upsert_requires_title_or_body() -> None:
    with pytest.raises(ValueError):
        await search.upsert("k")


async def test_upsert_rejects_empty_key() -> None:
    with pytest.raises(ValueError):
        await search.upsert("", title="t")


# ─── delete ────────────────────────────────────────────────────────────────


async def test_delete_url_encodes_namespace_and_key() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.delete(f"{BASE}/search/documents/space%2Fa/note%2F1").mock(
            return_value=httpx.Response(200, json={"deleted": True})
        )
        assert await search.delete("note/1", namespace="space/a") is True
    assert route.calls.last.request.method == "DELETE"


async def test_delete_returns_false_when_runtime_reports_no_op() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.delete(f"{BASE}/search/documents/default/missing").mock(
            return_value=httpx.Response(200, json={"deleted": False})
        )
        assert await search.delete("missing") is False


async def test_delete_validates_inputs() -> None:
    with pytest.raises(ValueError):
        await search.delete("")
    with pytest.raises(ValueError):
        await search.delete("k", namespace="")


# ─── namespace ─────────────────────────────────────────────────────────────


async def test_suite_search_namespace_matches_module() -> None:
    assert suite.search is search
    assert suite.search.search is search.search
    assert suite.search.upsert is search.upsert
    assert suite.search.delete is search.delete
