"""Context propagation tests.

The ``ctx`` proxy is built on ``contextvars``, so it must:

1. Reflect changes made by :func:`bind` and :func:`scope`.
2. Restore prior state when a scope exits.
3. Propagate into ``asyncio`` tasks spawned from within the scope.
4. Isolate concurrent tasks that bind their own contexts.
"""

from __future__ import annotations

import asyncio

import pytest

from af_stack import RequestContext, ctx, scope
from af_stack.ctx import bind, current, reset


def test_default_is_empty() -> None:
    snap = current()
    assert isinstance(snap, RequestContext)
    assert snap.tenant_id is None
    assert snap.user_id is None
    assert snap.api_key_id is None


def test_scope_sets_and_restores() -> None:
    assert ctx.tenant_id is None
    with scope(tenant_id="t_abc", user_id="u_xyz", api_key_id="k_1"):
        assert ctx.tenant_id == "t_abc"
        assert ctx.user_id == "u_xyz"
        assert ctx.api_key_id == "k_1"
        # Auto-generated request id when not provided.
        assert ctx.request_id.startswith("req_")
    assert ctx.tenant_id is None
    assert ctx.user_id is None


def test_request_id_passthrough() -> None:
    with scope(request_id="req_explicit"):
        assert ctx.request_id == "req_explicit"


def test_bind_reset_token() -> None:
    token = bind(tenant_id="t_1", request_id="req_one")
    try:
        assert ctx.tenant_id == "t_1"
        assert ctx.request_id == "req_one"
    finally:
        reset(token)
    assert ctx.tenant_id is None


def test_nested_scopes() -> None:
    with scope(tenant_id="t_outer"):
        assert ctx.tenant_id == "t_outer"
        with scope(tenant_id="t_inner"):
            assert ctx.tenant_id == "t_inner"
        assert ctx.tenant_id == "t_outer"
    assert ctx.tenant_id is None


async def test_context_propagates_into_child_tasks() -> None:
    captured: list[str | None] = []

    async def child() -> None:
        # Child task should see the parent's context.
        captured.append(ctx.tenant_id)
        captured.append(ctx.user_id)

    with scope(tenant_id="t_parent", user_id="u_parent"):
        await asyncio.create_task(child())

    assert captured == ["t_parent", "u_parent"]


async def test_concurrent_tasks_are_isolated() -> None:
    """Each asyncio task gets its own copy of the context."""

    async def worker(tenant: str, barrier: asyncio.Event) -> str | None:
        with scope(tenant_id=tenant):
            # Yield so both tasks are alive at the same time.
            await barrier.wait()
            return ctx.tenant_id

    barrier = asyncio.Event()
    t_a = asyncio.create_task(worker("t_a", barrier))
    t_b = asyncio.create_task(worker("t_b", barrier))
    await asyncio.sleep(0)
    barrier.set()
    results = await asyncio.gather(t_a, t_b)
    assert sorted(results) == ["t_a", "t_b"]  # type: ignore[type-var]
    # Outer scope was never bound, so the proxy is empty after gather.
    assert ctx.tenant_id is None


def test_proxy_repr_contains_fields() -> None:
    with scope(tenant_id="t_repr", user_id="u_repr", request_id="req_repr"):
        rendered = repr(ctx)
    assert "t_repr" in rendered
    assert "u_repr" in rendered
    assert "req_repr" in rendered


def test_with_returns_new_instance() -> None:
    base = RequestContext(tenant_id="t_a", request_id="req_a")
    updated = base.with_(tenant_id="t_b")
    assert base.tenant_id == "t_a"  # original unchanged (immutable)
    assert updated.tenant_id == "t_b"
    assert updated.request_id == "req_a"


@pytest.mark.parametrize(
    "kwargs,expected",
    [
        ({"tenant_id": "t1"}, "t1"),
        ({"user_id": "u1"}, None),
        ({}, None),
    ],
)
def test_scope_kwargs_only_apply_specified_fields(
    kwargs: dict[str, str], expected: str | None
) -> None:
    with scope(**kwargs):
        assert ctx.tenant_id == expected
