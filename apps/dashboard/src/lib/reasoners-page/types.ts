// SPDX-License-Identifier: Apache-2.0

import type { ReasonerAnalytics } from "@/lib/api"

export type ReasonerRow = ReasonerAnalytics["reasoners"][number]

// Window buckets the operator can pick from (?window=). Computed into a
// `from` ISO timestamp server-side — the analytics endpoint accepts
// from/to directly.
export type ReasonerWindowKind = "24h" | "7d" | "30d"

export function isReasonerWindowKind(v: string): v is ReasonerWindowKind {
  return v === "24h" || v === "7d" || v === "30d"
}

export interface ReasonersSnapshot {
  rows: ReasonerRow[]
  // Effective window echoed back by the backend; null when degraded.
  window: { from: string; to: string } | null
  fetchedAt: string
  healthy: boolean
}
