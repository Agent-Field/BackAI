// SPDX-License-Identifier: Apache-2.0

import type { CostRange, CostRangeKind } from "./types"

// Range helpers shared by server fetch + client polling. All ranges are
// UTC; the dashboard renders relative timestamps via the same formatter.

export function rangeFromKind(kind: CostRangeKind): CostRange {
  const now = new Date()
  const to = now.toISOString()
  const from = new Date(now.getTime() - daysFor(kind) * 86_400_000)
  // For "today" we anchor at UTC start-of-day so the comparison window
  // and the anchor live tile share a definition.
  if (kind === "today") {
    const start = new Date(
      Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()),
    )
    return { kind, from: start.toISOString(), to }
  }
  return { kind, from: from.toISOString(), to }
}

function daysFor(kind: CostRangeKind): number {
  switch (kind) {
    case "today":
      return 1
    case "7d":
      return 7
    case "30d":
      return 30
    case "90d":
      return 90
  }
}

export function isCostRangeKind(value: string): value is CostRangeKind {
  return value === "today" || value === "7d" || value === "30d" || value === "90d"
}
