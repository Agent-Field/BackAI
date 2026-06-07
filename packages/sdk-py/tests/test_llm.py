"""HTTP-mocked tests for ``af_stack.llm``.

Mirrors the wire contract declared in ``apps/dashboard/src/lib/api.ts``.
We cover:

* ``chat(stream=False)`` returns the full OpenAI-shaped response dict
* ``chat(stream=True)`` yields decoded SSE deltas until ``[DONE]``
* ``embed()`` posts ``{"model", "input", ...}`` and returns the response
* ``models()`` parses the model registry into typed Pydantic models
* The shared HTTP client forwards ``Authorization: Bearer <key>`` when
  ``AF_STACK_API_KEY`` is set — covers the per-tenant auth path.
"""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import llm, suite
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


@pytest.fixture(autouse=True)
async def reset_http_client(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "tk_llm_test")
    await http_close()
    yield
    await http_close()


# ─── chat (non-streaming) ─────────────────────────────────────────────────


async def test_chat_non_streaming_returns_full_response() -> None:
    response_body = {
        "id": "chatcmpl-abc123",
        "model": "qwen/qwen-2.5-72b-instruct",
        "object": "chat.completion",
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": "Hello back!"},
                "finish_reason": "stop",
            }
        ],
        "usage": {
            "prompt_tokens": 10,
            "completion_tokens": 4,
            "total_tokens": 14,
        },
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/llm/chat/completions").mock(
            return_value=httpx.Response(200, json=response_body)
        )
        result = await llm.chat(
            "qwen/qwen-2.5-72b-instruct",
            [{"role": "user", "content": "Hello"}],
            temperature=0.7,
        )
    assert result["id"] == "chatcmpl-abc123"
    assert result["choices"][0]["message"]["content"] == "Hello back!"
    assert result["usage"]["prompt_tokens"] == 10
    req = route.calls.last.request
    assert b"qwen/qwen-2.5-72b-instruct" in req.read()
    # The temperature kwarg must be forwarded into the body.
    assert b"temperature" in req.content
    # Authorization header is set from env (covers per-tenant auth).
    assert req.headers.get("authorization") == "Bearer tk_llm_test"


async def test_chat_rejects_empty_model() -> None:
    with pytest.raises(ValueError):
        await llm.chat("", [{"role": "user", "content": "hi"}])


async def test_chat_rejects_empty_messages() -> None:
    with pytest.raises(ValueError):
        await llm.chat("qwen/qwen-2.5-72b-instruct", [])


# ─── chat (streaming) ─────────────────────────────────────────────────────


async def test_chat_streaming_yields_deltas_and_stops_on_done() -> None:
    # Mimic OpenAI's SSE format: each event has a `data:` JSON line, the
    # stream terminates with `data: [DONE]`.
    sse_body = (
        'data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hel"}}]}\n\n'
        'data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"lo"}}]}\n\n'
        'data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"!"}}]}\n\n'
        'data: [DONE]\n\n'
    )
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/llm/chat/completions").mock(
            return_value=httpx.Response(
                200,
                headers={"content-type": "text/event-stream"},
                content=sse_body.encode(),
            )
        )
        iterator = await llm.chat(
            "qwen/qwen-2.5-72b-instruct",
            [{"role": "user", "content": "Hi"}],
            stream=True,
        )
        chunks = [c async for c in iterator]

    assert len(chunks) == 3
    pieces = [c["choices"][0]["delta"]["content"] for c in chunks]
    assert "".join(pieces) == "Hello!"


# ─── embed ────────────────────────────────────────────────────────────────


async def test_embed_posts_payload_and_returns_response() -> None:
    body = {
        "model": "text-embedding-3-small",
        "data": [{"index": 0, "embedding": [0.1, 0.2, 0.3]}],
        "usage": {"prompt_tokens": 4, "total_tokens": 4},
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/llm/embeddings").mock(
            return_value=httpx.Response(200, json=body)
        )
        result = await llm.embed("text-embedding-3-small", "hello world")
    assert result["data"][0]["embedding"] == [0.1, 0.2, 0.3]
    req = route.calls.last.request
    assert b"hello world" in req.read()
    assert b"text-embedding-3-small" in req.content


async def test_embed_accepts_batch_input() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/llm/embeddings").mock(
            return_value=httpx.Response(
                200,
                json={"model": "m", "data": [], "usage": {}},
            )
        )
        await llm.embed("m", ["a", "b", "c"])
    body = route.calls.last.request.read().decode()
    assert '"input"' in body
    assert "a" in body and "b" in body and "c" in body


async def test_embed_rejects_empty_input() -> None:
    with pytest.raises(ValueError):
        await llm.embed("m", [])


# ─── models ───────────────────────────────────────────────────────────────


async def test_models_parses_typed_response() -> None:
    body = {
        "models": [
            {
                "id": "qwen/qwen-2.5-72b-instruct",
                "display_name": "Qwen 2.5 72B Instruct",
                "provider": "openrouter",
                "prompt_usd_per_1m": 0.3,
                "completion_usd_per_1m": 0.3,
                "supports_streaming": True,
                "supports_tools": True,
            }
        ]
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/llm/models").mock(
            return_value=httpx.Response(200, json=body)
        )
        result = await llm.models()
    assert len(result.models) == 1
    m = result.models[0]
    assert m.id == "qwen/qwen-2.5-72b-instruct"
    assert m.supports_streaming is True
    assert m.prompt_usd_per_1m == 0.3


async def test_cache_stats_returns_typed_model() -> None:
    body = {
        "total_calls": 100,
        "cache_hits": 30,
        "cache_misses": 70,
        "hit_rate": 0.3,
        "savings_usd": 0.05,
        "entries": 12,
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/llm/cache/stats").mock(
            return_value=httpx.Response(200, json=body)
        )
        result = await llm.cache_stats()
    assert result.total_calls == 100
    assert result.hit_rate == 0.3


# ─── namespace wiring ─────────────────────────────────────────────────────


async def test_suite_llm_namespace() -> None:
    assert suite.llm is llm
    assert suite.llm.chat is llm.chat
    assert suite.llm.models is llm.models
