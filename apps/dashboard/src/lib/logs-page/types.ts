// SPDX-License-Identifier: Apache-2.0

import type { LogLine } from "@/lib/api"

// Logs page domain types. Level / service / search are server-side
// filters (the runtime's log store applies them before paging), so all
// three live in the URL and flow into every fetch.

export type LogLevelFilter = "all" | "debug" | "info" | "warn" | "error"

export function isLogLevelFilter(raw: string): raw is LogLevelFilter {
  return (
    raw === "all" ||
    raw === "debug" ||
    raw === "info" ||
    raw === "warn" ||
    raw === "error"
  )
}

export interface LogsFilters {
  level: LogLevelFilter
  service: string
  search: string
}

export const DEFAULT_LOGS_FILTERS: LogsFilters = {
  level: "all",
  service: "",
  search: "",
}

export interface LogsSnapshot {
  /** Newest first — see sortNewestFirst in ./derive. */
  logs: LogLine[]
  fetchedAt: string
  healthy: boolean
}
