// SPDX-License-Identifier: Apache-2.0

import type {
  AdminEventList,
  AdminServiceList,
  CostSummary,
  HomeOverview,
  ModulesState,
} from "@/lib/api"

/**
 * Composite snapshot of every endpoint the Home page reads. Built once
 * per server render by `getHomeSnapshot()` and handed to `<HomeShell>`.
 *
 * Every per-source value is nullable so a slow / failing endpoint
 * degrades that section's render rather than crashing the whole page.
 */
export interface HomeSnapshot {
  overview: HomeOverview | null
  costMTD: CostSummary | null
  services: AdminServiceList | null
  events: AdminEventList | null
  pendingApprovalsCount: number
  modules: ModulesState | null
  /** ISO timestamp when the snapshot was assembled. */
  fetchedAt: string
  /**
   * True when at least one of the auth-gated endpoints completed. False
   * when every fetch failed at the network layer — the dashboard renders
   * the "runtime unreachable" banner in that case.
   */
  runtimeReachable: boolean
  /** Per-source error messages, keyed by source name. */
  errors: Partial<Record<HomeSource, string>>
}

export type HomeSource =
  | "overview"
  | "cost"
  | "services"
  | "events"
  | "approvals"
  | "modules"

/** Status classifier used by every KPI tile + service pill. */
export type StatusState = "ok" | "watch" | "act" | "idle"

/** Three data states from home.md §10. Drives tile renderers. */
export type DataState = "live" | "empty" | "missing" | "degraded"

/**
 * Decorated KPI tile for the strip. Built client-side from
 * `HomeSnapshot` so the renderer is purely presentational.
 */
export interface KpiTileModel {
  id: KpiTileId
  label: string
  /** Numeric value when live; null when empty/missing/degraded. */
  value: number | null
  /** What unit to render (handled by the tile component). */
  unit: KpiUnit
  /** Last-hour sparkline buckets (length 24 when present). */
  sparkline: number[]
  state: DataState
  status: StatusState
  /** Optional sub-label rendered under the value. */
  subLabel?: string
  /**
   * Percent change vs prior window. Null when no prior data.
   * Server-side computed for the three sparkline-backed tiles; for the
   * rest the derive layer leaves this null (no delta surfacable).
   */
  deltaPct: number | null
}

export type KpiTileId =
  | "requests-per-minute"
  | "error-rate"
  | "cost-today"
  | "cost-mtd"
  | "queue-depth"
  | "live-runs"
  | "failed-24h"
  | "budget-consumed"

/**
 * How a tile renders its value.
 *
 *   count            — integer with thousands separators
 *   rate             — number with 1 decimal, no suffix
 *   percent          — number in 0..1 rendered as 0..100 %
 *   percent-as-whole — number already in 0..100, rendered as %
 *   currency-usd     — USD with auto-scale (cents for <$1, dollars otherwise)
 */
export type KpiUnit =
  | "count"
  | "rate"
  | "percent"
  | "percent-as-whole"
  | "currency-usd"
