// SPDX-License-Identifier: Apache-2.0

import type { ErrorCapabilities, ErrorGroup } from "@/lib/api"

// Errors page domain types. The backend lists one status bucket at a
// time (open|muted|resolved, defaulting to open), so the filter has no
// "all" value — it mirrors the API enum exactly.

export type ErrorStatusFilter = "open" | "muted" | "resolved"

export const DEFAULT_ERROR_STATUS: ErrorStatusFilter = "open"

export function isErrorStatusFilter(raw: string): raw is ErrorStatusFilter {
  return raw === "open" || raw === "muted" || raw === "resolved"
}

export interface ErrorsSnapshot {
  groups: ErrorGroup[]
  total: number
  /** Adapter capabilities — gates row actions + the volatility note. */
  capabilities: ErrorCapabilities | null
  fetchedAt: string
  healthy: boolean
}
