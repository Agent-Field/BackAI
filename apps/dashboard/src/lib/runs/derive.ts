// SPDX-License-Identifier: Apache-2.0

import type { Run } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"

import type {
  RunStatus,
  RunStatusFilter,
  RunTimeRange,
  RunsFilters,
} from "./types"

// Status helpers ---------------------------------------------------------

export function classifyRunStatus(status: RunStatus): StatusState {
  switch (status) {
    case "succeeded":
      return "ok"
    case "running":
    case "queued":
      return "watch"
    case "failed":
      return "act"
    case "cancelled":
      return "idle"
  }
}

export function isRunStatus(v: string): v is RunStatus {
  return (
    v === "queued" ||
    v === "running" ||
    v === "succeeded" ||
    v === "failed" ||
    v === "cancelled"
  )
}

export function isRunStatusFilter(v: string): v is RunStatusFilter {
  return v === "all" || isRunStatus(v)
}

export function isRunTimeRange(v: string): v is RunTimeRange {
  return v === "1h" || v === "24h" || v === "7d" || v === "30d" || v === "all"
}

// Client-side filters ----------------------------------------------------

// Gaps 27/28 — server doesn't support time range or search filters yet.
// We apply them client-side to whatever was fetched. Operators will see
// the backend's natural row count cap; until Gap 27 lands the page
// fetches up to 200 rows and lets the operator narrow from there.
export function applyClientFilters(
  runs: Run[],
  filters: RunsFilters,
): Run[] {
  const cutoff = cutoffForRange(filters.time)
  const needle = filters.search.trim().toLowerCase()
  return runs.filter((run) => {
    if (cutoff !== null) {
      const ts = Date.parse(run.started_at)
      if (!Number.isNaN(ts) && ts < cutoff) return false
    }
    if (needle) {
      const haystack = [
        run.id,
        run.agent,
        run.tenant_id ?? "",
        run.error ?? "",
      ]
        .join(" ")
        .toLowerCase()
      if (!haystack.includes(needle)) return false
    }
    return true
  })
}

function cutoffForRange(range: RunTimeRange): number | null {
  if (range === "all") return null
  const now = Date.now()
  switch (range) {
    case "1h":
      return now - 60 * 60_000
    case "24h":
      return now - 24 * 3_600_000
    case "7d":
      return now - 7 * 86_400_000
    case "30d":
      return now - 30 * 86_400_000
  }
}

// Counts per status (drives the filter chip badges) ----------------------

export interface StatusCounts {
  all: number
  queued: number
  running: number
  succeeded: number
  failed: number
  cancelled: number
}

export function statusCounts(runs: Run[]): StatusCounts {
  const out: StatusCounts = {
    all: runs.length,
    queued: 0,
    running: 0,
    succeeded: 0,
    failed: 0,
    cancelled: 0,
  }
  for (const r of runs) {
    out[r.status] += 1
  }
  return out
}

// Formatters -------------------------------------------------------------

export function formatRunDuration(ms?: number): string {
  if (!ms || ms <= 0) return "—"
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)}s`
  const minutes = Math.floor(ms / 60_000)
  const seconds = Math.round((ms % 60_000) / 1000)
  return `${minutes}m ${seconds}s`
}

export function formatRunCost(usd?: number): string {
  if (usd === undefined || usd === null) return "—"
  if (usd === 0) return "$0"
  if (usd < 0.01) return `$${usd.toFixed(4)}`
  if (usd < 100) return `$${usd.toFixed(2)}`
  return `$${Math.round(usd).toLocaleString()}`
}

export function formatRunAge(iso: string): string {
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = Math.max(0, Date.now() - ts)
  if (diffMs < 1000) return "now"
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86_400) return `${Math.floor(sec / 3600)}h ago`
  return new Date(ts).toISOString().slice(5, 16).replace("T", " ")
}

export function friendlyAgentName(raw: string): string {
  if (!raw) return "—"
  return raw.length > 40 ? `${raw.slice(0, 38)}…` : raw
}

export function shortRunId(id: string): string {
  return id.length > 12 ? id.slice(0, 12) : id
}
