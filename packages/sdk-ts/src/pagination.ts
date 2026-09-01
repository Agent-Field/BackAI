// SPDX-License-Identifier: Apache-2.0

// Auto-iterating pagination helpers — the TypeScript mirror of
// `af_stack/pagination.py` (kept 1:1 for cross-SDK parity).
//
// List endpoints across the suite paginate two ways: offset based
// (`limit`/`offset` with `total`/`has_more`) and cursor based (`next_token`).
// `AsyncPaginator` unifies both behind a single async-iterable so callers
// never hand-roll a fetch loop:
//
//   import { BackAI, paginate } from "@af-stack/sdk"
//
//   const client = new BackAI()
//   const pager = paginate<Job>(async (cursor) => {
//     const offset = (cursor as number) ?? 0
//     const page = await client.jobs.list({ offset, limit: 50 })
//     const next = page.hasMore ? offset + page.jobs.length : null
//     return [page.jobs, next]
//   })
//   for await (const job of pager) console.log(job.id)
//
// The page fetcher is deliberately transport-agnostic: you provide the page
// fetch, it walks the cursors.

/**
 * A page fetcher takes the current cursor (`null` for the first page) and
 * returns `[items, nextCursor]`. A `nextCursor` of `null` ends iteration.
 */
export type PageFetcher<T> = (cursor: unknown) => Promise<[T[], unknown]> | [T[], unknown]

/** Wrap a page fetcher into an async-iterable over individual items. */
export class AsyncPaginator<T> implements AsyncIterable<T> {
  private readonly fetchPage: PageFetcher<T>
  private readonly startCursor: unknown

  constructor(fetchPage: PageFetcher<T>, startCursor: unknown = null) {
    this.fetchPage = fetchPage
    this.startCursor = startCursor
  }

  async *[Symbol.asyncIterator](): AsyncIterator<T> {
    let cursor = this.startCursor
    while (true) {
      const [items, nextCursor] = await this.fetchPage(cursor)
      for (const item of items) yield item
      if (nextCursor === null || nextCursor === undefined) break
      cursor = nextCursor
    }
  }

  /** Iterate page-by-page instead of item-by-item. */
  async *pages(): AsyncIterable<T[]> {
    let cursor = this.startCursor
    while (true) {
      const [items, nextCursor] = await this.fetchPage(cursor)
      yield items
      if (nextCursor === null || nextCursor === undefined) break
      cursor = nextCursor
    }
  }

  /** Eagerly drain the paginator into an array (optionally capped). */
  async collect(opts: { limit?: number } = {}): Promise<T[]> {
    const out: T[] = []
    for await (const item of this) {
      out.push(item)
      if (opts.limit !== undefined && out.length >= opts.limit) break
    }
    return out
  }
}

/**
 * Return an {@link AsyncPaginator} over `fetchPage`. `fetchPage(cursor)` must
 * return `[items, nextCursor]`; a `nextCursor` of `null` stops iteration.
 */
export function paginate<T>(
  fetchPage: PageFetcher<T>,
  opts: { startCursor?: unknown } = {},
): AsyncPaginator<T> {
  return new AsyncPaginator<T>(fetchPage, opts.startCursor ?? null)
}
