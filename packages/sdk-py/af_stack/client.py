# SPDX-License-Identifier: Apache-2.0

"""Explicit, configurable BackAI client.

``BackAI`` is the counterpart to the env-configured ``suite.*`` singletons:
instead of reading ``AF_STACK_URL`` / ``AF_STACK_API_KEY`` from the process
environment, it takes explicit ``base_url`` / ``api_key`` / ``timeout`` /
``max_retries`` and exposes the same operational namespaces::

    from af_stack import BackAI

    client = BackAI(base_url="https://api.example.com", api_key="sk-...")
    result = await client.agents.call("supportdesk.echo", {"payload": {"m": "hi"}})
    await client.close()

Every namespace method is transparently bound to the client's own
:class:`af_stack._http.Transport` for the duration of the call (via a
``contextvars`` slot), so the existing namespace modules target the client's
base URL / key / retry policy with no per-module rewiring. The env-configured
``suite`` path is unchanged.

The set of exposed namespaces + methods is the cross-language parity contract
in ``packages/sdk-parity.json`` — the parity test asserts this client's
surface matches it exactly, so a method added in only one SDK fails tests.
"""

from __future__ import annotations

import functools
import inspect
from collections.abc import Callable
from typing import Any

from . import (
    _http,
    agents,
    approvals,
    audio,
    auth,
    billing,
    cost,
    harnesses,
    images,
    jobs,
    llm,
    memory,
    notifications,
    oauth,
    realtime,
    runs,
    sandbox,
    search,
    shipwright,
    storage,
    tools,
    webhooks,
)

# Governed namespaces: name -> (module, [python attribute names]). This is the
# authoritative surface the explicit client exposes; it MUST agree with
# ``packages/sdk-parity.json`` (enforced by tests/test_parity.py).
_GOVERNED: dict[str, tuple[Any, list[str]]] = {
    "agents": (
        agents,
        [
            "approve",
            "call",
            "call_async",
            "cancel",
            "deny",
            "pending_approvals",
            "status",
            "stream",
        ],
    ),
    "approvals": (approvals, ["decide", "get", "list", "request"]),
    "audio": (audio, ["speech", "transcribe", "translate"]),
    "auth": (auth, ["whoami"]),
    "billing": (
        billing,
        [
            "checkout",
            "customer",
            "customers",
            "entitlements",
            "has_budget",
            "meter",
            "meters",
            "plans",
            "portal_link",
        ],
    ),
    "cost": (cost, ["events"]),
    "harnesses": (harnesses, ["get", "list", "probe"]),
    "images": (images, ["edit", "generate", "variations"]),
    "jobs": (jobs, ["enqueue", "get", "list", "retry"]),
    "llm": (llm, ["cache_stats", "chat", "embed", "models"]),
    "memory": (memory, ["delete", "get", "list", "put", "search"]),
    "notifications": (notifications, ["email", "get", "list", "send", "stats"]),
    "oauth": (oauth, ["authorize_url", "connected", "disconnect", "token"]),
    "realtime": (realtime, ["subscribe"]),
    "runs": (runs, ["subscribe"]),
    "sandbox": (sandbox, ["get", "list", "pool", "run", "stop"]),
    "search": (search, ["delete", "search", "upsert"]),
    "shipwright": (shipwright, ["complete", "create", "get", "list"]),
    "storage": (storage, ["delete", "download", "list", "signed_url", "upload"]),
    "tools": (
        tools,
        [
            "add_mcp_server",
            "call_adapter",
            "call_mcp",
            "enable_mcp_server",
            "list_adapters",
            "list_mcp_servers",
            "list_mcp_tools",
            "remove_mcp_server",
            "set_adapter_enabled",
        ],
    ),
    "webhooks": (
        webhooks,
        [
            "emit",
            "get",
            "list",
            "retry",
            "send",
            "subscribe",
            "subscriptions",
            "unsubscribe",
        ],
    ),
}

GOVERNED_NAMESPACES: tuple[str, ...] = tuple(sorted(_GOVERNED))


class _Namespace:
    """A namespace on an explicit client: bound methods and nothing else."""

    def __init__(self, methods: dict[str, Callable[..., Any]]) -> None:
        for name, fn in methods.items():
            setattr(self, name, fn)

    def __repr__(self) -> str:
        names = ", ".join(sorted(vars(self)))
        return f"<BackAI namespace [{names}]>"


class BackAI:
    """An explicit BackAI suite client.

    Parameters
    ----------
    base_url:
        Runtime base URL. Defaults to ``AF_STACK_URL`` (``http://localhost:8080``).
    api_key:
        Bearer key. Defaults to ``AF_STACK_API_KEY``.
    timeout:
        Per-request timeout in seconds (default 30).
    max_retries:
        Automatic retries for transient failures (429/5xx). Defaults to 2.
        Only safe methods (GET/HEAD/OPTIONS) retry automatically; mutating
        calls retry solely when an ``idempotency_key`` is supplied.
    check_runtime_version:
        When true (default), lazily fetch ``GET /version`` once and warn on a
        major version mismatch. Tolerates a 404.
    """

    agents: _Namespace
    approvals: _Namespace
    audio: _Namespace
    auth: _Namespace
    billing: _Namespace
    cost: _Namespace
    harnesses: _Namespace
    images: _Namespace
    jobs: _Namespace
    llm: _Namespace
    memory: _Namespace
    notifications: _Namespace
    oauth: _Namespace
    realtime: _Namespace
    runs: _Namespace
    sandbox: _Namespace
    search: _Namespace
    shipwright: _Namespace
    storage: _Namespace
    tools: _Namespace
    webhooks: _Namespace

    def __init__(
        self,
        *,
        base_url: str | None = None,
        api_key: str | None = None,
        timeout: float | None = None,
        max_retries: int | None = None,
        check_runtime_version: bool = True,
        transport: _http.Transport | None = None,
    ) -> None:
        if transport is not None:
            self._transport = transport
        else:
            self._transport = _http.Transport(
                base_url=base_url if base_url is not None else _http._base_url(),
                api_key=api_key if api_key is not None else _http._api_key(),
                timeout=timeout if timeout is not None else _http.DEFAULT_TIMEOUT_S,
                max_retries=(max_retries if max_retries is not None else _http.DEFAULT_MAX_RETRIES),
                check_version=check_runtime_version,
            )
        self._build_namespaces()

    # -- introspection ----------------------------------------------------
    @property
    def base_url(self) -> str:
        return self._transport.base_url

    @property
    def max_retries(self) -> int:
        return self._transport.max_retries

    def _build_namespaces(self) -> None:
        for ns_name, (module, method_names) in _GOVERNED.items():
            bound = {name: self._bind(getattr(module, name)) for name in method_names}
            setattr(self, ns_name, _Namespace(bound))

    def _bind(self, fn: Callable[..., Any]) -> Callable[..., Any]:
        """Wrap ``fn`` so it runs against this client's transport."""
        transport = self._transport

        if inspect.isasyncgenfunction(fn):

            @functools.wraps(fn)
            async def agen_wrapper(*args: Any, **kwargs: Any) -> Any:
                token = _http.use_transport(transport)
                try:
                    await transport.ensure_version_checked()
                    async for item in fn(*args, **kwargs):
                        yield item
                finally:
                    _http.reset_transport(token)

            return agen_wrapper

        @functools.wraps(fn)
        async def coro_wrapper(*args: Any, **kwargs: Any) -> Any:
            token = _http.use_transport(transport)
            try:
                await transport.ensure_version_checked()
                return await fn(*args, **kwargs)
            finally:
                _http.reset_transport(token)

        return coro_wrapper

    async def runtime_version(self) -> str | None:
        """Fetch the runtime version (``None`` when the endpoint is absent)."""
        token = _http.use_transport(self._transport)
        try:
            body = await self._transport.request_json("GET", "/version")
        except _http.AFStackError as exc:
            if exc.status_code == 404:
                return None
            raise
        finally:
            _http.reset_transport(token)
        if isinstance(body, dict):
            raw = body.get("version") or body.get("runtime_version")
            return str(raw) if raw is not None else None
        return None

    async def close(self) -> None:
        """Close the underlying HTTP transport."""
        await self._transport.aclose()

    async def __aenter__(self) -> BackAI:
        return self

    async def __aexit__(self, *exc: object) -> None:
        await self.close()


__all__ = ["GOVERNED_NAMESPACES", "BackAI"]
