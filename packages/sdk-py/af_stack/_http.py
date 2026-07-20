# SPDX-License-Identifier: Apache-2.0

"""Internal async HTTP transport for the AF Stack Suite SDK.

The SDK is driven by a :class:`Transport` — an object that owns a base URL,
an optional API key, a request timeout, and a retry policy, plus a lazily
created :class:`httpx.AsyncClient`. Two ways to reach a transport:

* **Env-configured default.** The module-level helpers (:func:`request_json`,
  :func:`stream_sse`, :func:`get_client`) resolve a process-wide default
  transport built from ``AF_STACK_URL`` / ``AF_STACK_API_KEY``. This is what
  the ``suite.*`` / ``from af_stack import agents`` convenience path uses, and
  its behaviour is unchanged from earlier releases (no automatic retries).

* **Explicit client.** :class:`af_stack.BackAI` builds its own transport and
  binds it into a ``contextvars`` slot for the duration of every delegated
  call, so the existing namespace modules transparently target the explicit
  client's base URL / key / retry policy without any per-module rewiring.

Errors raise :class:`AFStackError`, which carries the structured ``code``,
``message``, ``request_id`` / ``status`` and ``details`` shape defined in
``TECH-SPEC.md §13``.
"""

from __future__ import annotations

import asyncio
import datetime as _dt
import json as _json
import os
import random
import re
import warnings
from collections.abc import AsyncIterator
from contextvars import ContextVar
from email.utils import parsedate_to_datetime
from typing import Any

import httpx

from . import ctx as _ctx_module

DEFAULT_BASE_URL = "http://localhost:8080"
API_PREFIX = "/api/v1"
DEFAULT_TIMEOUT_S = 30.0
# Explicit ``BackAI`` clients retry transient failures by default. The
# env-configured default transport keeps the legacy behaviour (no retries).
DEFAULT_MAX_RETRIES = 2

# Status codes worth retrying: rate limiting + transient upstream failures.
_RETRYABLE_STATUS = frozenset({429, 500, 502, 503, 504})
# Only inherently-idempotent methods retry automatically; mutations retry
# solely when the caller supplies an ``idempotency_key`` (which also sends the
# ``Idempotency-Key`` header so the server can dedupe).
_IDEMPOTENT_METHODS = frozenset({"GET", "HEAD", "OPTIONS"})

_RETRY_BASE_DELAY_S = 0.5
_RETRY_MAX_DELAY_S = 20.0

# ---------------------------------------------------------------------------
# Runtime-version compatibility policy.
#
# The SDK lazily fetches ``GET /api/v1/version`` once per explicit client and
# *warns* (never fails) when the runtime's major version falls outside the
# supported range. 404 (endpoint absent on older runtimes) is tolerated.
# ---------------------------------------------------------------------------
SUPPORTED_RUNTIME_RANGE = ">=0.0.0,<1.0.0"
SUPPORTED_RUNTIME_MAJOR = 0

_SEMVER_RE = re.compile(r"\s*v?(\d+)\.(\d+)\.(\d+)")


def check_runtime_compat(version: str | None) -> str | None:
    """Return a warning message if ``version`` is major-incompatible, else None.

    Pure and side-effect free so it can be unit-tested without a network.
    """
    if not version:
        return None
    match = _SEMVER_RE.match(str(version))
    if match is None:
        return None
    major = int(match.group(1))
    if major != SUPPORTED_RUNTIME_MAJOR:
        return (
            f"BackAI runtime version {version} (major {major}) is outside the "
            f"range this SDK supports ({SUPPORTED_RUNTIME_RANGE}). Behaviour may "
            f"be incompatible — upgrade the SDK."
        )
    return None


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

    @property
    def status(self) -> int:
        """Alias for :attr:`status_code` — parity with the TS ``SuiteError``."""
        return self.status_code

    def __repr__(self) -> str:
        return (
            f"AFStackError(code={self.code!r}, status={self.status_code}, "
            f"request_id={self.request_id!r})"
        )


def _base_url() -> str:
    return os.environ.get("AF_STACK_URL", DEFAULT_BASE_URL).rstrip("/")


def _api_key() -> str | None:
    return os.environ.get("AF_STACK_API_KEY")


def _synthetic_traceparent(request_id: str) -> str:
    # Cheap OTel-style trace propagation: a synthetic traceparent built from
    # the request id. A real OTel SDK will overwrite when present.
    return f"00-{(request_id.replace('req_', '') + '0' * 32)[:32]}-{'0' * 16}-01"


def _retry_delay(response: httpx.Response, attempt: int) -> float:
    """Compute the sleep before the next attempt, honouring ``Retry-After``."""
    retry_after = response.headers.get("retry-after")
    if retry_after:
        # Numeric seconds …
        try:
            return max(0.0, min(float(retry_after), _RETRY_MAX_DELAY_S))
        except ValueError:
            # … or an HTTP-date.
            try:
                when = parsedate_to_datetime(retry_after)
                now = _dt.datetime.now(tz=when.tzinfo) if when.tzinfo else _dt.datetime.now()
                delta = (when - now).total_seconds()
                if delta > 0:
                    return min(delta, _RETRY_MAX_DELAY_S)
            except (TypeError, ValueError):
                pass
    # Exponential backoff with full jitter.
    ceiling = min(_RETRY_BASE_DELAY_S * (2**attempt), _RETRY_MAX_DELAY_S)
    return random.uniform(0.0, ceiling)


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


class Transport:
    """Owns connection config + an :class:`httpx.AsyncClient` for one client."""

    def __init__(
        self,
        *,
        base_url: str,
        api_key: str | None = None,
        timeout: float = DEFAULT_TIMEOUT_S,
        max_retries: int = 0,
        check_version: bool = False,
        httpx_transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout
        self.max_retries = max(0, int(max_retries))
        self.check_version = check_version
        self._httpx_transport = httpx_transport
        self._client: httpx.AsyncClient | None = None
        self._version_checked = False

    @property
    def client(self) -> httpx.AsyncClient:
        if self._client is None:
            kwargs: dict[str, Any] = {
                "base_url": self.base_url + API_PREFIX,
                "timeout": self.timeout,
            }
            if self._httpx_transport is not None:
                kwargs["transport"] = self._httpx_transport
            self._client = httpx.AsyncClient(**kwargs)
        return self._client

    def build_headers(
        self,
        extra: dict[str, str] | None = None,
        *,
        idempotency_key: str | None = None,
    ) -> dict[str, str]:
        snap = _ctx_module.current()
        request_id = snap.request_id or _ctx_module._new_request_id()
        headers: dict[str, str] = {
            "accept": "application/json",
            "x-request-id": request_id,
        }
        if self.api_key:
            headers["authorization"] = f"Bearer {self.api_key}"
        if snap.tenant_id:
            headers["x-af-stack-tenant-id"] = snap.tenant_id
        if snap.user_id:
            headers["x-af-stack-user-id"] = snap.user_id
        if snap.api_key_id:
            headers["x-af-stack-api-key-id"] = snap.api_key_id
        headers.setdefault("traceparent", _synthetic_traceparent(request_id))
        if idempotency_key:
            headers["idempotency-key"] = idempotency_key
        if extra:
            headers.update(extra)
        return headers

    async def request_json(
        self,
        method: str,
        path: str,
        *,
        json: Any | None = None,
        params: dict[str, Any] | None = None,
        headers: dict[str, str] | None = None,
        timeout: float | None = None,
        idempotency_key: str | None = None,
    ) -> Any:
        """Perform an HTTP request (with retries) and return the JSON body."""
        method_u = method.upper()
        retryable = method_u in _IDEMPOTENT_METHODS or idempotency_key is not None
        req_headers = self.build_headers(headers, idempotency_key=idempotency_key)
        eff_timeout = timeout if timeout is not None else self.timeout

        attempt = 0
        while True:
            response = await self.client.request(
                method_u,
                path,
                json=json,
                params=params,
                headers=req_headers,
                timeout=eff_timeout,
            )
            if response.status_code < 400:
                if not response.content:
                    return None
                return response.json()
            if (
                attempt < self.max_retries
                and retryable
                and response.status_code in _RETRYABLE_STATUS
            ):
                await asyncio.sleep(_retry_delay(response, attempt))
                attempt += 1
                continue
            _raise_for_error(response)

    async def stream_sse(
        self,
        method: str,
        path: str,
        *,
        json: Any | None = None,
        params: dict[str, Any] | None = None,
        headers: dict[str, str] | None = None,
    ) -> AsyncIterator[dict[str, Any]]:
        sse_headers = self.build_headers(headers)
        sse_headers["accept"] = "text/event-stream"

        event_name = "message"
        data_buffer: list[str] = []

        async with self.client.stream(
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
                    continue
                if line.startswith("event:"):
                    event_name = line[len("event:") :].strip()
                elif line.startswith("data:"):
                    data_buffer.append(line[len("data:") :].lstrip())

    async def ensure_version_checked(self) -> None:
        """Fetch ``GET /version`` once and warn on a major mismatch.

        Tolerant of 404 (older runtimes) and every transport error — the check
        never blocks or fails a real call.
        """
        if self._version_checked or not self.check_version:
            return
        self._version_checked = True
        version: str | None = None
        try:
            response = await self.client.request(
                "GET",
                "/version",
                headers=self.build_headers(),
                timeout=self.timeout,
            )
            if response.status_code < 400 and response.content:
                body = response.json()
                if isinstance(body, dict):
                    raw = body.get("version") or body.get("runtime_version")
                    version = str(raw) if raw is not None else None
        except Exception:  # noqa: BLE001 — version probe must never raise
            version = None
        message = check_runtime_compat(version)
        if message:
            warnings.warn(message, RuntimeWarning, stacklevel=2)

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()
            self._client = None


# ---------------------------------------------------------------------------
# Ambient transport resolution.
#
# ``_active`` holds the explicit client's transport for the duration of a
# delegated call (set by ``af_stack.client.BackAI``). When unset, module-level
# helpers fall back to the env-configured process default.
# ---------------------------------------------------------------------------
_active: ContextVar[Transport | None] = ContextVar("af_stack.transport", default=None)
_default: Transport | None = None


def _default_transport() -> Transport:
    global _default
    if _default is None:
        _default = Transport(
            base_url=_base_url(),
            api_key=_api_key(),
            timeout=DEFAULT_TIMEOUT_S,
            max_retries=0,
            check_version=False,
        )
    return _default


def current_transport() -> Transport:
    """Return the active (explicit-client) transport, or the env default."""
    active = _active.get()
    return active if active is not None else _default_transport()


def use_transport(transport: Transport):  # type: ignore[no-untyped-def]
    """Bind ``transport`` as active; returns a token for :func:`reset_transport`."""
    return _active.set(transport)


def reset_transport(token) -> None:  # type: ignore[no-untyped-def]
    _active.reset(token)


def get_client() -> httpx.AsyncClient:
    """Return the active transport's shared async client."""
    return current_transport().client


def _build_headers(extra: dict[str, str] | None = None) -> dict[str, str]:
    return current_transport().build_headers(extra)


async def request_json(
    method: str,
    path: str,
    *,
    json: Any | None = None,
    params: dict[str, Any] | None = None,
    headers: dict[str, str] | None = None,
    timeout: float | None = None,
    idempotency_key: str | None = None,
) -> Any:
    """Perform an HTTP request via the active transport and return JSON.

    ``path`` is relative to ``/api/v1``.
    """
    return await current_transport().request_json(
        method,
        path,
        json=json,
        params=params,
        headers=headers,
        timeout=timeout,
        idempotency_key=idempotency_key,
    )


async def stream_sse(
    method: str,
    path: str,
    *,
    json: Any | None = None,
    params: dict[str, Any] | None = None,
    headers: dict[str, str] | None = None,
) -> AsyncIterator[dict[str, Any]]:
    """Iterate parsed SSE events from the given endpoint via the active transport.

    Each yielded value is a dict like ``{"event": "...", "data": {...}}``.
    """
    async for event in current_transport().stream_sse(
        method,
        path,
        json=json,
        params=params,
        headers=headers,
    ):
        yield event


async def close() -> None:
    """Close the process-default transport. Call on graceful shutdown."""
    global _default
    if _default is not None:
        await _default.aclose()
        _default = None


__all__ = [
    "API_PREFIX",
    "DEFAULT_BASE_URL",
    "DEFAULT_MAX_RETRIES",
    "DEFAULT_TIMEOUT_S",
    "SUPPORTED_RUNTIME_RANGE",
    "AFStackError",
    "Transport",
    "check_runtime_compat",
    "close",
    "current_transport",
    "get_client",
    "request_json",
    "reset_transport",
    "stream_sse",
    "use_transport",
]
