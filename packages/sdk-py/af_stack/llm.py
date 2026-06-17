# SPDX-License-Identifier: Apache-2.0

"""``suite.llm.*`` — OpenAI-compatible LLM gateway operations.

Every model call inside the suite flows through this gateway. The wire
shape mirrors the OpenAI Chat Completions / Embeddings API so callers
can use any OpenAI-compatible client (incl. the ``openai`` npm/pypi
package) by pointing it at ``/api/v1/llm``. The SDK methods below are
ergonomic shortcuts around the same surface.

==================================  =========================================
SDK call                            Endpoint
==================================  =========================================
:func:`chat`                        ``POST   /api/v1/llm/chat/completions``
:func:`embed`                       ``POST   /api/v1/llm/embeddings``
:func:`models`                      ``GET    /api/v1/llm/models``
==================================  =========================================

The :class:`LLMModel`, :class:`LLMModelList` and :class:`CacheStats`
Pydantic models mirror the zod schemas in ``apps/dashboard/src/lib/api.ts``.

When ``ctx.api_key`` is set (or ``AF_STACK_API_KEY`` env), the shared
HTTP client forwards it via the ``Authorization`` header so per-tenant
auth works without any extra plumbing.
"""

from __future__ import annotations

from collections.abc import AsyncIterator
from typing import Any

from pydantic import BaseModel, ConfigDict, Field

from . import _http


# ─── Pydantic mirrors of LLM gateway schemas ──────────────────────────────


class LLMModel(BaseModel):
    """One model entry returned by :func:`models`.

    Mirrors ``LLMModelSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    id: str
    display_name: str
    provider: str
    prompt_usd_per_1m: float
    completion_usd_per_1m: float
    supports_streaming: bool
    supports_tools: bool


class LLMModelList(BaseModel):
    """Paginated list of available models.

    Mirrors ``LLMModelListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    models: list[LLMModel] = Field(default_factory=list)


class CacheStats(BaseModel):
    """Aggregated cache stats for the gateway.

    Mirrors ``CacheStatsSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    total_calls: int
    cache_hits: int
    cache_misses: int
    hit_rate: float
    savings_usd: float
    entries: int


# ─── Public API ───────────────────────────────────────────────────────────


async def chat(
    model: str,
    messages: list[dict[str, Any]],
    *,
    stream: bool = False,
    reasoner: str | None = None,
    **kwargs: Any,
) -> Any:
    """Call an LLM via the OpenAI-compatible chat completions endpoint.

    ``model`` is the gateway-routable id (e.g. ``"qwen/qwen-2.5-72b-instruct"``).
    ``messages`` follows the standard OpenAI chat-completions shape.

    When ``stream=False`` (default) this returns the full response dict
    (id, model, choices, usage, ...). When ``stream=True`` it returns an
    async iterator yielding raw SSE-decoded deltas — the same chunks an
    OpenAI streaming client would observe.

    Additional kwargs (``temperature``, ``max_tokens``, ``tools``, …) are
    forwarded verbatim into the request body.
    """
    if not model:
        raise ValueError("model must be a non-empty string")
    if not isinstance(messages, list) or not messages:
        raise ValueError("messages must be a non-empty list of dicts")

    payload: dict[str, Any] = {"model": model, "messages": messages}
    for k, v in kwargs.items():
        if v is not None:
            payload[k] = v

    headers = {"X-AF-Reasoner": reasoner} if reasoner else None

    if stream:
        payload["stream"] = True
        return _stream_chat(payload, headers=headers)

    body = await _http.request_json(
        "POST", "/llm/chat/completions", json=payload, headers=headers
    )
    return body or {}


async def _stream_chat(
    payload: dict[str, Any],
    *,
    headers: dict[str, str] | None = None,
) -> AsyncIterator[dict[str, Any]]:
    """Internal: iterate streaming chat deltas as JSON dicts."""
    async for event in _http.stream_sse(
        "POST", "/llm/chat/completions", json=payload, headers=headers
    ):
        data = event.get("data")
        # OpenAI streams emit a terminal "[DONE]" sentinel — surface it
        # so callers can decide whether to break early, but don't try to
        # JSON-decode it.
        if data == "[DONE]" or data == {"done": True}:
            break
        if isinstance(data, dict):
            yield data


async def embed(
    model: str,
    input: str | list[str],  # noqa: A002 — mirrors OpenAI's ``input`` parameter
    *,
    reasoner: str | None = None,
    **kwargs: Any,
) -> dict[str, Any]:
    """Compute embeddings via the OpenAI-compatible embeddings endpoint.

    ``input`` can be a single string or a batch. Returns the full response
    dict (``data[].embedding``, ``model``, ``usage``).
    """
    if not model:
        raise ValueError("model must be a non-empty string")
    if input is None or (isinstance(input, list) and not input):
        raise ValueError("input must be a non-empty string or list")
    payload: dict[str, Any] = {"model": model, "input": input}
    for k, v in kwargs.items():
        if v is not None:
            payload[k] = v
    headers = {"X-AF-Reasoner": reasoner} if reasoner else None
    body = await _http.request_json(
        "POST", "/llm/embeddings", json=payload, headers=headers
    )
    return body or {}


async def models() -> LLMModelList:
    """List all models exposed by the gateway, with per-million pricing."""
    body = await _http.request_json("GET", "/llm/models")
    return LLMModelList.model_validate(body or {"models": []})


async def cache_stats() -> CacheStats:
    """Fetch aggregated cache statistics for the gateway."""
    body = await _http.request_json("GET", "/llm/cache/stats")
    return CacheStats.model_validate(
        body
        or {
            "total_calls": 0,
            "cache_hits": 0,
            "cache_misses": 0,
            "hit_rate": 0.0,
            "savings_usd": 0.0,
            "entries": 0,
        }
    )


__all__ = [
    "CacheStats",
    "LLMModel",
    "LLMModelList",
    "cache_stats",
    "chat",
    "embed",
    "models",
]
