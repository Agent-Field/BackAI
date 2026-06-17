// SPDX-License-Identifier: Apache-2.0

// Client-side derivations from the raw HomeSnapshot.
//
// Most of the home page is pure presentation — the only intelligence here
// is mapping numeric KPI values to a StatusState (ok / watch / act) and
// constructing the 8 KPI tile models the strip renders. Keeping this
// logic in a separate module makes it testable and stops it from leaking
// into JSX.

import type { HomeSnapshot, KpiTileModel, StatusState } from "./types"

/** Build the 8 KPI tile models that drive the strip. */
export function deriveKpiTiles(snapshot: HomeSnapshot): KpiTileModel[] {
  const o = snapshot.overview
  const cost = snapshot.costMTD
  const haveOverview = o !== null
  const haveCost = cost !== null

  return [
    {
      id: "requests-per-minute",
      label: "Requests / min",
      value: haveOverview ? o.requests_per_minute : null,
      unit: "rate",
      sparkline: haveOverview ? o.request_sparkline : [],
      state: haveOverview ? "live" : "degraded",
      status: "idle",
    },
    {
      id: "error-rate",
      label: "Error rate (60m)",
      value: haveOverview ? o.error_rate : null,
      unit: "percent",
      sparkline: haveOverview ? o.error_sparkline : [],
      state: haveOverview ? "live" : "degraded",
      // Thresholds from development/ux/page-design-framework.md §8:
      // 2% watch, 5% act. Rate is 0..1 from the backend.
      status: haveOverview ? classifyErrorRate(o.error_rate) : "idle",
    },
    {
      id: "cost-today",
      label: "Cost today",
      value: haveOverview ? o.cost_today_usd : null,
      unit: "currency-usd",
      sparkline: haveOverview ? o.cost_sparkline : [],
      state: haveOverview ? "live" : "degraded",
      status: "idle",
    },
    {
      id: "cost-mtd",
      label: "Cost MTD",
      value: haveCost ? cost.period_total_usd : null,
      unit: "currency-usd",
      sparkline: [],
      state: haveCost ? "live" : "degraded",
      status: "idle",
    },
    {
      id: "queue-depth",
      label: "Queue depth",
      value: haveOverview ? o.queue_depth : null,
      unit: "count",
      sparkline: haveOverview ? o.queue_sparkline : [],
      state: haveOverview ? "live" : "degraded",
      status: "idle",
    },
    {
      id: "live-runs",
      label: "Live runs",
      value: haveOverview ? o.live_runs : null,
      unit: "count",
      sparkline: [],
      state: haveOverview ? "live" : "degraded",
      status: "idle",
      subLabel: "Background jobs running",
    },
    {
      id: "failed-24h",
      label: "Failed 24h",
      value: haveOverview ? o.failed_runs_last_24h : null,
      unit: "count",
      sparkline: haveOverview ? o.error_sparkline : [],
      state: haveOverview ? "live" : "degraded",
      status: haveOverview
        ? classifyFailedRuns(o.failed_runs_last_24h)
        : "idle",
    },
    {
      id: "budget-consumed",
      label: "Budget consumed",
      value: haveOverview ? o.budgets_aggregate.avg_consumed_pct : null,
      unit: "percent-as-whole",
      sparkline: [],
      state: deriveBudgetState(snapshot),
      status: haveOverview
        ? classifyBudgetConsumed(o.budgets_aggregate.avg_consumed_pct)
        : "idle",
      subLabel: haveOverview
        ? budgetSubLabel(o.budgets_aggregate)
        : undefined,
    },
  ]
}

/** Error rate thresholds in 0..1. */
function classifyErrorRate(rate: number): StatusState {
  if (rate >= 0.05) return "act"
  if (rate >= 0.02) return "watch"
  return "ok"
}

/** Failed run count classifier (24h). Conservative thresholds for v1. */
function classifyFailedRuns(count: number): StatusState {
  if (count >= 50) return "act"
  if (count >= 10) return "watch"
  return "ok"
}

/** Budget consumed % thresholds per framework §8. */
function classifyBudgetConsumed(pct: number): StatusState {
  if (pct >= 100) return "act"
  if (pct >= 90) return "watch"
  return "ok"
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
  if (agg.tenant_count === 0) return "No budgets set"
  const tenants =
    agg.tenant_count === 1 ? "1 tenant" : `${agg.tenant_count} tenants`
  if (agg.tenants_at_risk > 0) {
    return `${agg.tenants_at_risk} at risk · ${tenants}`
  }
  return tenants
}
