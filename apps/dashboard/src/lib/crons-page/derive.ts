// SPDX-License-Identifier: Apache-2.0

import type { Cron } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"

import type { CronActiveFilter } from "./types"

// Status + filter helpers -----------------------------------------------

// Crons map onto the dashboard's four-tone status scale by their toggle:
// an active schedule reads "ok" (green — it's doing its job), a paused one
// reads "idle" (dimmed — deliberately off, not broken).
export function classifyCron(cron: Cron): StatusState {
  return cron.is_active ? "ok" : "idle"
}

export function isCronActiveFilter(v: string): v is CronActiveFilter {
  return v === "all" || v === "active" || v === "inactive"
}

export function matchesActiveFilter(cron: Cron, filter: CronActiveFilter): boolean {
  if (filter === "active") return cron.is_active
  if (filter === "inactive") return !cron.is_active
  return true
}

// The KPI "Next fire" tile is the earliest next_run_at across the crons
// that are still active — a paused cron's next_run_at is stale.
export function nextFireAt(crons: Cron[]): string | null {
  let earliest: number | null = null
  for (const cron of crons) {
    if (!cron.is_active) continue
    const ts = Date.parse(cron.next_run_at)
    if (Number.isNaN(ts)) continue
    if (earliest === null || ts < earliest) earliest = ts
  }
  return earliest === null ? null : new Date(earliest).toISOString()
}

// Formatters -------------------------------------------------------------

// Relative time that reads both directions off the same clock: past ticks
// render "…ago", future ticks (next_run_at) render "in …". Sub-minute
// resolution collapses to "now" / "soon" so the column doesn't flicker.
export function formatRelativeTime(iso: string | null): string {
  if (!iso) return "—"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = ts - Date.now()
  const past = diffMs <= 0
  const sec = Math.floor(Math.abs(diffMs) / 1000)
  if (sec < 30) return past ? "now" : "soon"
  const value =
    sec < 60
      ? `${sec}s`
      : sec < 3600
        ? `${Math.floor(sec / 60)}m`
        : sec < 86_400
          ? `${Math.floor(sec / 3600)}h`
          : `${Math.floor(sec / 86_400)}d`
  return past ? `${value} ago` : `in ${value}`
}

// Absolute clock for the detail panel (UTC, minute resolution).
export function formatClock(iso: string | null): string {
  if (!iso) return "—"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  return new Date(ts).toISOString().slice(0, 16).replace("T", " ") + " UTC"
}

export function shortCronId(id: string): string {
  return id.length > 12 ? id.slice(0, 12) : id
}

export function safeStringifyArgs(value: unknown): string {
  if (value === undefined || value === null) return ""
  if (typeof value === "object" && Object.keys(value).length === 0) return ""
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}
