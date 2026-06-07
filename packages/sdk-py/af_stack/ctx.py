# SPDX-License-Identifier: Apache-2.0

"""Request context for the AF Stack Suite SDK.

The ``ctx`` module-level proxy reads from contextvars so it works across
async boundaries (asyncio tasks, ``anyio`` task groups, etc.). Middleware
on every entry point (HTTP, job, cron, webhook) is responsible for
calling :func:`bind` to seed the context for the duration of a request.

Usage::

    from af_stack import ctx

    tenant_id = ctx.tenant_id
    user_id   = ctx.user_id
    rid       = ctx.request_id

Tests and short-lived scopes can use the :func:`scope` context manager::

    with ctx.scope(tenant_id="t_abc", user_id="u_xyz"):
        await suite.agents.call(...)
"""

from __future__ import annotations

import contextvars
import uuid
from collections.abc import Iterator
from contextlib import contextmanager
from dataclasses import dataclass, replace


@dataclass(frozen=True, slots=True)
class RequestContext:
    """Immutable snapshot of the current request context.

    All fields are optional except ``request_id`` which is auto-generated
    when missing so log correlation always works.
    """

    tenant_id: str | None = None
    user_id: str | None = None
    api_key_id: str | None = None
    request_id: str = ""
    job_id: str | None = None
    attempt: int | None = None

    def with_(self, **fields: object) -> RequestContext:
        """Return a new context with the given fields overridden."""
        return replace(self, **fields)  # type: ignore[arg-type]


_EMPTY = RequestContext(request_id="")
_current: contextvars.ContextVar[RequestContext] = contextvars.ContextVar(
    "af_stack.ctx", default=_EMPTY
)


def current() -> RequestContext:
    """Return the current context (or an empty default)."""
    return _current.get()


def bind(
    *,
    tenant_id: str | None = None,
    user_id: str | None = None,
    api_key_id: str | None = None,
    request_id: str | None = None,
    job_id: str | None = None,
    attempt: int | None = None,
) -> contextvars.Token[RequestContext]:
    """Set the current context. Returns a token for ``reset()``.

    Prefer :func:`scope` in application code; ``bind`` is for framework
    middleware that owns explicit lifecycle.
    """
    rid = request_id or _new_request_id()
    new_ctx = RequestContext(
        tenant_id=tenant_id,
        user_id=user_id,
        api_key_id=api_key_id,
        request_id=rid,
        job_id=job_id,
        attempt=attempt,
    )
    return _current.set(new_ctx)


def reset(token: contextvars.Token[RequestContext]) -> None:
    """Restore the context captured by a prior :func:`bind` token."""
    _current.reset(token)


@contextmanager
def scope(
    *,
    tenant_id: str | None = None,
    user_id: str | None = None,
    api_key_id: str | None = None,
    request_id: str | None = None,
    job_id: str | None = None,
    attempt: int | None = None,
) -> Iterator[RequestContext]:
    """Context manager that binds and automatically restores prior state."""
    token = bind(
        tenant_id=tenant_id,
        user_id=user_id,
        api_key_id=api_key_id,
        request_id=request_id,
        job_id=job_id,
        attempt=attempt,
    )
    try:
        yield current()
    finally:
        reset(token)


def _new_request_id() -> str:
    return "req_" + uuid.uuid4().hex[:16]


class _CtxProxy:
    """Module-level proxy that always reads from the live contextvar.

    Attribute access (``ctx.tenant_id``) always reflects the *current*
    context, so the proxy works correctly after async task switches.
    """

    __slots__ = ()

    @property
    def tenant_id(self) -> str | None:
        return _current.get().tenant_id

    @property
    def user_id(self) -> str | None:
        return _current.get().user_id

    @property
    def api_key_id(self) -> str | None:
        return _current.get().api_key_id

    @property
    def request_id(self) -> str:
        return _current.get().request_id

    @property
    def job_id(self) -> str | None:
        return _current.get().job_id

    @property
    def attempt(self) -> int | None:
        return _current.get().attempt

    # Re-export the helpers so users can do ``ctx.scope(...)``.
    def current(self) -> RequestContext:
        return current()

    def bind(self, **kwargs: object) -> contextvars.Token[RequestContext]:
        return bind(**kwargs)  # type: ignore[arg-type]

    def reset(self, token: contextvars.Token[RequestContext]) -> None:
        reset(token)

    def scope(self, **kwargs: object):  # type: ignore[no-untyped-def]
        return scope(**kwargs)  # type: ignore[arg-type]

    def __repr__(self) -> str:
        c = _current.get()
        return (
            f"<af_stack.ctx tenant_id={c.tenant_id!r} "
            f"user_id={c.user_id!r} request_id={c.request_id!r}>"
        )


ctx = _CtxProxy()
"""Module-level proxy. Import as ``from af_stack import ctx``."""

__all__ = [
    "RequestContext",
    "bind",
    "ctx",
    "current",
    "reset",
    "scope",
]