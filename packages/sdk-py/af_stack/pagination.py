# SPDX-License-Identifier: Apache-2.0

"""Auto-iterating pagination helpers.

List endpoints across the suite paginate two ways: **offset** based
(``limit`` / ``offset`` with ``total`` / ``has_more``) and **cursor** based
(``next_token``). :class:`AsyncPaginator` unifies both behind a single
async-iterable so callers never hand-roll a fetch loop::

    from af_stack import BackAI, paginate

    client = BackAI()

    async def _page(cursor):
        result = await client.jobs.list(offset=cursor or 0, limit=50)
        next_cursor = (cursor or 0) + len(result.jobs) if result.has_more else None
        return result.jobs, next_cursor

    async for job in paginate(_page):
        print(job.id)

The :func:`paginate` helper is deliberately transport-agnostic — you provide
the page fetch, it walks the cursors. This mirrors the TypeScript
``paginate`` helper 1:1 for cross-SDK parity.
"""

from __future__ import annotations

from collections.abc import AsyncIterator, Awaitable, Callable
from typing import Generic, TypeVar

T = TypeVar("T")
Cursor = object

# A page fetcher takes the current cursor (``None`` for the first page) and
# returns ``(items, next_cursor)``. A ``next_cursor`` of ``None`` ends
# iteration.
PageFetcher = Callable[[object | None], Awaitable["tuple[list[T], object | None]"]]


class AsyncPaginator(Generic[T]):
    """Wrap a page fetcher into an async-iterable over individual items."""

    def __init__(
        self,
        fetch_page: PageFetcher[T],
        *,
        start_cursor: object | None = None,
    ) -> None:
        self._fetch_page = fetch_page
        self._start_cursor = start_cursor

    def __aiter__(self) -> AsyncIterator[T]:
        return self._iterate()

    async def _iterate(self) -> AsyncIterator[T]:
        cursor = self._start_cursor
        while True:
            items, next_cursor = await self._fetch_page(cursor)
            for item in items:
                yield item
            if next_cursor is None:
                break
            cursor = next_cursor

    async def pages(self) -> AsyncIterator[list[T]]:
        """Iterate page-by-page instead of item-by-item."""
        cursor = self._start_cursor
        while True:
            items, next_cursor = await self._fetch_page(cursor)
            yield items
            if next_cursor is None:
                break
            cursor = next_cursor

    async def collect(self, *, limit: int | None = None) -> list[T]:
        """Eagerly drain the paginator into a list (optionally capped)."""
        out: list[T] = []
        async for item in self:
            out.append(item)
            if limit is not None and len(out) >= limit:
                break
        return out


def paginate(
    fetch_page: PageFetcher[T],
    *,
    start_cursor: object | None = None,
) -> AsyncPaginator[T]:
    """Return an :class:`AsyncPaginator` over ``fetch_page``.

    ``fetch_page(cursor)`` must return ``(items, next_cursor)``; a
    ``next_cursor`` of ``None`` stops iteration.
    """
    return AsyncPaginator(fetch_page, start_cursor=start_cursor)


__all__ = ["AsyncPaginator", "paginate"]
