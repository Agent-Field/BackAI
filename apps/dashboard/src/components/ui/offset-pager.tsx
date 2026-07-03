// SPDX-License-Identifier: Apache-2.0

"use client"

import { ChevronLeft, ChevronRight } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"

import { Button } from "@/components/ui/button"

// OffsetPager — Prev/Next pagination for offset-paged admin lists
// (audit log, activity log, …). The offset lives in the URL (?offset=N)
// so pages stay linkable and the server component refetches on change.
// Built for People → Audit / Activity; reusable by any offset-paged
// surface.

interface OffsetPagerProps {
  /** Current offset (0-based). */
  offset: number
  /** Rows requested per page. */
  pageSize: number
  /** Rows actually shown on this page. */
  count: number
  /** Total matching rows as reported by the backend. */
  total: number
  /** Backend has_more flag — gates the Next button. */
  hasMore: boolean
}

export function OffsetPager({
  offset,
  pageSize,
  count,
  total,
  hasMore,
}: OffsetPagerProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const go = (nextOffset: number) => {
    const params = new URLSearchParams(searchParams.toString())
    if (nextOffset <= 0) params.delete("offset")
    else params.set("offset", String(nextOffset))
    const qs = params.toString()
    // push (not replace) so Back walks through pages.
    router.push(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
  }

  const from = count === 0 ? 0 : offset + 1
  const to = offset + count

  return (
    <div className="flex items-center justify-between gap-stack border-t px-row-x py-row-y">
      <span className="font-mono text-meta tabular-nums text-muted-foreground">
        {count === 0 ? `0 of ${total}` : `${from}–${to} of ${total}`}
      </span>
      <div className="flex items-center gap-inline">
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={offset <= 0}
          onClick={() => go(Math.max(0, offset - pageSize))}
          className="h-7 gap-inline text-meta"
        >
          <ChevronLeft className="size-3.5" aria-hidden />
          Prev
        </Button>
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={!hasMore}
          onClick={() => go(offset + pageSize)}
          className="h-7 gap-inline text-meta"
        >
          Next
          <ChevronRight className="size-3.5" aria-hidden />
        </Button>
      </div>
    </div>
  )
}
