// SPDX-License-Identifier: Apache-2.0

"use client"

import { useCallback } from "react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"

import { FilterChip, FilterChipGroup } from "@/components/ui/filter-chip"

import type { ReasonerWindowKind } from "@/lib/reasoners-page/types"

// URL-persistent window selector (?window=24h|7d|30d). The page is
// force-dynamic, so replacing the search param re-renders the server
// table with the widened window — no client polling shell needed here.
// Reuses the standard FilterChip primitive so contrast + density match
// Runs / Cost / Health.

const OPTIONS: { value: ReasonerWindowKind; label: string }[] = [
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
]

export function WindowChips({ window }: { window: ReasonerWindowKind }) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const setWindow = useCallback(
    (next: ReasonerWindowKind) => {
      const params = new URLSearchParams(searchParams.toString())
      // Default window stays out of the URL so shared links stay clean.
      if (next === "24h") params.delete("window")
      else params.set("window", next)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [pathname, router, searchParams],
  )

  return (
    <FilterChipGroup label="Window">
      {OPTIONS.map((opt) => (
        <FilterChip
          key={opt.value}
          label={opt.label}
          active={window === opt.value}
          onSelect={() => setWindow(opt.value)}
        />
      ))}
    </FilterChipGroup>
  )
}
