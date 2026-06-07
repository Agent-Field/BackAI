# SPDX-License-Identifier: Apache-2.0

"""Internal async HTTP client for the AF Stack Suite SDK.

A single shared ``httpx.AsyncClient`` is lazy-initialised on first use and
reused for the process lifetime. Base URL comes from ``AF_STACK_URL``
(default ``http://localhost:8080``); API key from ``AF_STACK_API_KEY``
when set.

All requests propagate the W3C ``traceparent`` and ``x-request-id``
headers from the active :mod:`af_stack.ctx`. Tenant and user context
travel as ``x-af-stack-tenant-id`` / ``x-af-stack-user-id`` so the
runtime can resolve scope without re-authenticating.

Errors raise :class:`AFStackError`, which carries the structured ``code``,
``message``, ``request_id`` and ``details`` shape defined in
``TECH-SPEC.md §13``.
"""

from __future__ import annotations

import json as _json
import os
from collections.abc import AsyncIterator
from typing import Any

import httpx

from . import ctx as _ctx_module

DEFAULT_BASE_URL = "http://localhost:8080"
API_PREFIX = "/api/v1"
DEFAULT_TIMEOUT_S = 30.0


class AFStackError(Exception):
    """Structured error returned by the AF Stack runtime."""

    def __init__(
        self,
        *,
        code: str,
        message: str,
        status_code: int,
        request_id: str | None = None,
        details: dict[str, Any] | None = None,
    ) -> None:
        super().__init__(f"[{code}] {message}")
        self.code = code
        self.message = message
        self.status_code = status_code
        self.request_id = request_id
        self.details = details or {}

    def __repr__(self) -> str:
        return (
            f"AFStackError(code={self.code!r}, status={self.status_code}, "
            f"request_id={self.request_id!r})"
        )


_client: httpx.AsyncClient | None = None


def _base_url() -> str:
    return os.environ.get("AF_STACK_URL", DEFAULT_BASE_URL).rstrip("/")


def _api_key() -> str | None:
    return os.environ.get("AF_STACK_API_KEY")


def get_client() -> httpx.AsyncClient:
    """Return the shared async client, creating it on first call."""
    global _client
    if _client is None:
        _client = httpx.AsyncClient(
            base_url=_base_url() + API_PREFIX,
            timeout=DEFAULT_TIMEOUT_S,
        )
    return _client


async def close() -> None:
    """Close the shared client. Call on graceful shutdown."""
    global _client
    if _client is not None:
        await _client.aclose()
        _client = None


def _build_headers(extra: dict[str, str] | None = None) -> dict[str, str]:
    snap = _ctx_module.current()
    headers: dict[str, str] = {
        "accept": "application/json",
        "x-request-id": snap.request_id or _ctx_module._new_request_id(),
    }
    key = _api_key()
    if key:
        headers["authorization"] = f"Bearer {key}"
    if snap.tenant_id:
        headers["x-af-stack-tenant-id"] = snap.tenant_id
    if snap.user_id:
        headers["x-af-stack-user-id"] = snap.user_id
    if snap.api_key_id:
        headers["x-af-stack-api-key-id"] = snap.api_key_id
    # Cheap OTel-style trace propagation: a synthetic traceparent built
    # from the request id. Real OTel SDK will overwrite when present.
    headers.setdefault(
        "traceparent",
        f"00-{(headers['x-request-id'].replace('req_', '') + '0' * 32)[:32]}"
        f"-{'0' * 16}-01",
    )
    if extra:
        headers.update(extra)
    return headers


def _raise_for_error(response: httpx.Response) -> None:
    if response.status_code < 400:
        return
    code = "HTTP_ERROR"
    message = response.text or response.reason_phrase or "request failed"
    details: dict[str, Any] = {}
    request_id: str | None = response.headers.get("x-request-id")
    # Best-effort parse of the structured error envelope.
    try:
        payload = response.json()
    except ValueError:
        payload = None
    if isinstance(payload, dict) and isinstance(payload.get("error"), dict):
        err = payload["error"]
        code = str(err.get("code") or code)
        message = str(err.get("message") or message)
        details = dict(err.get("details") or {})
        request_id = err.get("request_id") or request_id
    raise AFStackError(
        code=code,
        message=message,
        status_code=response.status_code,
        request_id=request_id,
        details=details,
    )


async def request_json(
    method: str,
    path: str,
    *,
    json: Any | None = None,
    params: dict[str, Any] | None = None,
    headers: dict[str, str] | None = None,
    timeout: float | None = None,
) -> Any:
    """Perform an HTTP request and return the decoded JSON body.

    ``path`` is relative to ``/api/v1``. Pass an absolute path to bypass.
    """
    client = get_client()
    response = await client.request(
        method,
        path,
        json=json,
        params=params,
        headers=_build_headers(headers),
        timeout=timeout if timeout is not None else DEFAULT_TIMEOUT_S,
    )
    _raise_for_error(response)
    if not response.content:
        return None
    return response.json()


async def stream_sse(
    method: str,
    path: str,
    *,
    json: Any | None = None,
    params: dict[str, Any] | None = None,
    headers: dict[str, str] | None = None,
) -> AsyncIterator[dict[str, Any]]:
    """Iterate parsed SSE events from the given endpoint.

    Each yielded value is a dict like ``{"event": "...", "data": {...}}``.
    Lines that don't decode as JSON are surfaced as raw ``str`` data.
    """
    client = get_client()
    sse_headers = _build_headers(headers)
    sse_headers["accept"] = "text/event-stream"

    event_name = "message"
    data_buffer: list[str] = []

    async with client.stream(
        method,
        path,
        json=json,
        params=params,
        headers=sse_headers,
        timeout=None,
    ) as response:
        _raise_for_error(response)
        async for line in response.aiter_lines():
            if line == "":
                if data_buffer:
                    raw = "\n".join(data_buffer)
                    data_buffer = []
                    try:
                        data: Any = _json.loads(raw)
                    except ValueError:
                        data = raw
                    yield {"event": event_name, "data": data}
                event_name = "message"
                continue
            if line.startswith(":"):
                # Comment / keepalive.
                continue
            if line.startswith("event:"):
                event_name = line[len("event:") :].strip()
            elif line.startswith("data:"):
                data_buffer.append(line[len("data:") :].lstrip())


__all__ = [
    "AFStackError",
    "API_PREFIX",
    "DEFAULT_BASE_URL",
    "DEFAULT_TIMEOUT_S",
    "close",
    "get_client",
    "request_json",
    "stream_sse",
]