# SPDX-License-Identifier: Apache-2.0

"""Behaviour tests for the auto-iterating pagination helper.

Validation contract:
* An offset-style fetcher (limit/offset + has_more) is walked to exhaustion.
* A cursor-style fetcher (next_token) is walked to exhaustion.
* Iteration stops as soon as ``next_cursor`` is ``None`` (no extra fetch).
* ``collect(limit=...)`` caps the number of items drained.
"""

from __future__ import annotations

from af_stack import AsyncPaginator, paginate


async def test_offset_pagination_walks_all_pages() -> None:
    pages = [[1, 2, 3], [4, 5, 6], [7]]
    fetched: list[object] = []

    async def fetch(cursor: object | None) -> tuple[list[int], object | None]:
        idx = cursor or 0
        fetched.append(idx)
        items = pages[idx]
        next_cursor = idx + 1 if idx + 1 < len(pages) else None
        return items, next_cursor

    collected = [item async for item in paginate(fetch)]
    assert collected == [1, 2, 3, 4, 5, 6, 7]
    assert fetched == [0, 1, 2]


async def test_cursor_pagination_stops_on_none() -> None:
    store = {
        None: (["a", "b"], "c1"),
        "c1": (["c", "d"], "c2"),
        "c2": (["e"], None),
    }
    calls = 0

    async def fetch(cursor: object | None) -> tuple[list[str], object | None]:
        nonlocal calls
        calls += 1
        return store[cursor]  # type: ignore[index]

    result = await paginate(fetch).collect()
    assert result == ["a", "b", "c", "d", "e"]
    assert calls == 3


async def test_collect_respects_limit() -> None:
    async def fetch(cursor: object | None) -> tuple[list[int], object | None]:
        idx = cursor or 0
        return [idx], idx + 1  # infinite stream

    paginator: AsyncPaginator[int] = paginate(fetch)
    first_three = await paginator.collect(limit=3)
    assert first_three == [0, 1, 2]


async def test_pages_iterates_by_page() -> None:
    pages = [["x"], ["y", "z"]]

    async def fetch(cursor: object | None) -> tuple[list[str], object | None]:
        idx = cursor or 0
        return pages[idx], (idx + 1 if idx + 1 < len(pages) else None)

    seen = [page async for page in paginate(fetch).pages()]
    assert seen == [["x"], ["y", "z"]]
