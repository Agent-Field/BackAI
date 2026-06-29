// SPDX-License-Identifier: Apache-2.0

import type { HomeSnapshot, KpiTileModel, StatusState } from "./types"

// Decorate HomeSnapshot into the eight KpiTileModel rows the strip
// renders. All status thresholds, sub-labels, and delta sourcing live
// here so the JSX is purely presentational.

export function deriveKpiTiles(snapshot: HomeSnapshot): KpiTileModel[] {
  const o = snapshot.overview
  const cost = snapshot.costMTD
  const haveOverview = o !== null
  const haveCost = cost !== null

  return [
    {
      id: "requests-per-minute",
      label: "RPM",
      value: haveOverview ? o.requests_per_minute : null,
      unit: "rate",
      sparkline: haveOverview ? o.request_sparkline : [],
      state: haveOverview ? "live" : "degraded",
      status: classifyByValue(
        haveOverview ? o.requests_per_minute : null,
        "non-zero-is-ok",
      ),
      deltaPct: haveOverview ? safeDelta(o.request_delta_pct) : null,
    },
    {
      id: "error-rate",
      label: "ERR%",
      value: haveOverview ? o.error_rate : null,
      unit: "percent",
      sparkline: haveOverview ? o.error_sparkline : [],
      state: haveOverview ? "live" : "degraded",
      // Threshold per page-design-framework §8: 2% watch, 5% act.
      status: haveOverview ? classifyErrorRate(o.error_rate) : "idle",
      deltaPct: haveOverview ? safeDelta(o.error_delta_pct) : null,
    },
    {
      id: "cost-today",
      label: "COST",
      value: haveOverview ? o.cost_today_usd : null,
      unit: "currency-usd",
      sparkline: haveOverview ? o.cost_sparkline : [],
      state: haveOverview ? "live" : "degraded",
      status: "idle",
      subLabel:
        haveOverview && o.cost_today_usd > 0 ? "today (UTC)" : undefined,
      deltaPct: haveOverview ? safeDelta(o.cost_delta_pct) : null,
    },
    {
      id: "cost-mtd",
      label: "MTD",
      value: haveCost ? cost.period_total_usd : null,
      unit: "currency-usd",
      sparkline: [],
      state: haveCost ? "live" : "degraded",
      status: "idle",
      deltaPct: null,
      subLabel: haveCost ? "month-to-date" : undefined,
    },
    {
      id: "queue-depth",
      label: "QUEUE",
      value: haveOverview ? o.queue_depth : null,
      unit: "count",
      sparkline: haveOverview ? o.queue_sparkline : [],
      state: haveOverview ? "live" : "degraded",
      status: classifyByValue(
        haveOverview ? o.queue_depth : null,
        "zero-is-ok",
      ),
      deltaPct: null,
    },
    {
      id: "live-runs",
      label: "LIVE",
      value: haveOverview ? o.live_runs : null,
      unit: "count",
      sparkline: [],
      state: haveOverview ? "live" : "degraded",
      status: classifyByValue(
        haveOverview ? o.live_runs : null,
        "non-zero-is-ok",
      ),
      deltaPct: null,
    },
    {
      id: "failed-24h",
      label: "FAIL24",
      value: haveOverview ? o.failed_runs_last_24h : null,
      unit: "count",
      sparkline: haveOverview ? o.error_sparkline : [],
      state: haveOverview ? "live" : "degraded",
      status: haveOverview
        ? classifyFailedRuns(o.failed_runs_last_24h)
        : "idle",
      deltaPct: null,
    },
    {
      id: "budget-consumed",
      label: "BUDGET",
      value: haveOverview ? o.budgets_aggregate.avg_consumed_pct : null,
      unit: "percent-as-whole",
      sparkline: [],
      state: deriveBudgetState(snapshot),
      status: haveOverview
        ? classifyBudgetConsumed(o.budgets_aggregate.avg_consumed_pct)
        : "idle",
      deltaPct: null,
      subLabel: haveOverview
        ? budgetSubLabel(o.budgets_aggregate)
        : undefined,
    },
  ]
}

/**
 * Drives the delta colour direction per tile. RPM up = good; error/cost/
 * fail24 up = bad; everything else neutral.
 */
export const KPI_GOOD_DIRECTION: Record<
  KpiTileModel["id"],
  "up" | "down" | "neutral"
> = {
  "requests-per-minute": "up",
  "error-rate": "down",
  "cost-today": "neutral",
  "cost-mtd": "neutral",
  "queue-depth": "down",
  "live-runs": "neutral",
  "failed-24h": "down",
  "budget-consumed": "down",
}

function safeDelta(n: number): number | null {
  if (!Number.isFinite(n)) return null
  return n
}

function classifyByValue(
  v: number | null,
  rule: "zero-is-ok" | "non-zero-is-ok",
): StatusState {
  if (v === null) return "idle"
  if (rule === "zero-is-ok") return v === 0 ? "ok" : "watch"
  return v > 0 ? "ok" : "idle"
}

function classifyErrorRate(rate: number): StatusState {
  if (rate >= 0.05) return "act"
  if (rate >= 0.02) return "watch"
  if (rate > 0) return "ok"
  return "idle"
}

function classifyFailedRuns(count: number): StatusState {
  if (count >= 50) return "act"
  if (count >= 10) return "watch"
  if (count > 0) return "watch"
  // Zero failures is genuinely "idle" — no signal — not "green-go". Per the
  // critique, a green dot on Failed 24h: 0 is misleading.
  return "idle"
}

function classifyBudgetConsumed(pct: number): StatusState {
  if (pct >= 100) return "act"
  if (pct >= 90) return "watch"
  if (pct > 0) return "ok"
  return "idle"
}

function deriveBudgetState(snapshot: HomeSnapshot) {
  if (snapshot.overview === null) return "degraded" as const
  if (snapshot.overview.budgets_aggregate.tenant_count === 0) {
    return "empty" as const
  }
  return "live" as const
}

function budgetSubLabel(agg: {
  tenants_at_risk: number
  tenant_count: number
}): string | undefined {
  if (agg.tenant_count === 0) return "no budgets set"
  const tenants =
    agg.tenant_count === 1 ? "1 tenant" : `${agg.tenant_count} tenants`
  if (agg.tenants_at_risk > 0) {
    return `${agg.tenants_at_risk} at risk · ${tenants}`
  }
  return tenants
}
