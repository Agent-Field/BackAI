// SPDX-License-Identifier: Apache-2.0

import type { Run } from "@/lib/api"

export type RunStatus = Run["status"] // "queued" | "running" | "succeeded" | "failed" | "cancelled"

export type RunStatusFilter = RunStatus | "all"

// Time range buckets the operator can pick from. All ranges are computed
// client-side until Gap 27 (from/to) lands on the backend.
export type RunTimeRange = "1h" | "24h" | "7d" | "30d" | "all"

export interface RunsFilters {
  status: RunStatusFilter
  time: RunTimeRange
  search: string
}

export const DEFAULT_FILTERS: RunsFilters = {
  status: "all",
  time: "24h",
  search: "",
}

export interface RunsSnapshot {
  runs: Run[]
  total: number
  hasMore: boolean
  fetchedAt: string
  healthy: boolean
}
