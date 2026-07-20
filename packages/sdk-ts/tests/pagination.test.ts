// SPDX-License-Identifier: Apache-2.0

// Behaviour tests for the auto-iterating pagination helper (mirror of
// af_stack/tests/test_pagination.py).
//
// Validation contract:
// * An offset-style fetcher (limit/offset + hasMore) is walked to exhaustion.
// * A cursor-style fetcher (nextToken) is walked to exhaustion.
// * Iteration stops as soon as nextCursor is null (no extra fetch).
// * collect({ limit }) caps the number of items drained.

import { describe, expect, it } from "vitest"
import { AsyncPaginator, paginate } from "../src/index.js"

describe("paginate", () => {
  it("walks all pages (offset style)", async () => {
    const pages = [[1, 2, 3], [4, 5, 6], [7]]
    const fetched: number[] = []
    const collected: number[] = []
    for await (const item of paginate<number>((cursor) => {
      const idx = (cursor as number | null) ?? 0
      fetched.push(idx)
      const next = idx + 1 < pages.length ? idx + 1 : null
      return [pages[idx], next]
    })) {
      collected.push(item)
    }
    expect(collected).toEqual([1, 2, 3, 4, 5, 6, 7])
    expect(fetched).toEqual([0, 1, 2])
  })

  it("stops on a null cursor (cursor style) with no extra fetch", async () => {
    const store: Record<string, [string[], string | null]> = {
      __start__: [["a", "b"], "c1"],
      c1: [["c", "d"], "c2"],
      c2: [["e"], null],
    }
    let calls = 0
    const result = await paginate<string>((cursor) => {
      calls += 1
      const key = (cursor as string | null) ?? "__start__"
      return store[key]
    }).collect()
    expect(result).toEqual(["a", "b", "c", "d", "e"])
    expect(calls).toBe(3)
  })

  it("collect respects the limit on an infinite stream", async () => {
    const pager: AsyncPaginator<number> = paginate<number>((cursor) => {
      const idx = (cursor as number | null) ?? 0
      return [[idx], idx + 1]
    })
    expect(await pager.collect({ limit: 3 })).toEqual([0, 1, 2])
  })

  it("pages() iterates page-by-page", async () => {
    const pages = [["x"], ["y", "z"]]
    const seen: string[][] = []
    for await (const page of paginate<string>((cursor) => {
      const idx = (cursor as number | null) ?? 0
      return [pages[idx], idx + 1 < pages.length ? idx + 1 : null]
    }).pages()) {
      seen.push(page)
    }
    expect(seen).toEqual([["x"], ["y", "z"]])
  })
})
