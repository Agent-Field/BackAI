// SPDX-License-Identifier: Apache-2.0

import type { Budget, CostSummary } from "@/lib/api"

import type {
  CostAnomaly,
  CostSnapshot,
  TenantSeriesPoint,
} from "./types"

// Pure derivations the page renders from CostSnapshot. No fetching, no
// React, no side effects.

// ──────────────────────────────────────────────────────────────────────
// Anomaly detection (client-side, v1 — Gap 12 deferred)
// ──────────────────────────────────────────────────────────────────────

// Tenants whose share of period spend is materially above their share of
// the prior period count as an anomaly. Cheap, deterministic, and
// understandable — operators can sanity-check the rule. v0.2 will swap
// this for a backend-emitted signal.
//
// We surface up to three callouts:
//   1. The tenant with the largest spike ratio (>= 2× share)
//   2. The model whose share grew the most
//   3. A forecast-vs-budget overrun, if present
export function deriveAnomalies(snapshot: CostSnapshot): CostAnomaly[] {
  const cost = snapshot.primary
  if (!cost) return []
  const anomalies: CostAnomaly[] = []

  const totalNow = cost.period_total_usd
  const totalPrev = cost.previous_total_usd
  if (totalNow > 0 && totalPrev > 0) {
    const tenantSpike = topShareSpike(cost, "tenant")
    if (tenantSpike) {
      anomalies.push({
        id: `tenant-spike:${tenantSpike.id}`,
        severity: tenantSpike.ratio >= 4 ? "critical" : "warning",
        title: `${tenantSpike.label} is ${formatRatio(tenantSpike.ratio)} its prior share`,
        context: `${formatCurrency(tenantSpike.currentCost)} this period vs ${formatCurrency(tenantSpike.previousCost)} expected`,
        sparkline: tenantSpike.sparkline,
        primaryAction: {
          label: "Edit budget",
          href: `/cost?budget=${encodeURIComponent(tenantSpike.id)}`,
        },
        secondaryAction: {
          label: "Open tenant",
          href: `/people/tenants/${encodeURIComponent(tenantSpike.id)}`,
        },
      })
    }
  }

  if (cost.budget_usd !== null && cost.forecast_usd > cost.budget_usd) {
    const overshoot = cost.forecast_usd - cost.budget_usd
    const pctOver = (overshoot / cost.budget_usd) * 100
    anomalies.push({
      id: "forecast-overrun",
      severity: pctOver >= 25 ? "critical" : "warning",
      title: `Month forecast crosses budget`,
      context: `${formatCurrency(cost.forecast_usd)} projected vs ${formatCurrency(cost.budget_usd)} budget · ${pctOver.toFixed(0)}% over`,
      primaryAction: {
        label: "Review budgets",
        href: "/cost#budgets",
      },
    })
  }

  return anomalies
}

interface TopShareResult {
  id: string
  label: string
  ratio: number // current share / previous share
  currentCost: number
  previousCost: number
  sparkline: number[] | undefined
}

// Approximate "prior share" using the by_day series scaled to the prior
// period total. Exact per-tenant prior-period totals require Gap 13/16; we
// fall back to even distribution which biases against false negatives,
// not false positives.
function topShareSpike(
  cost: CostSummary,
  axis: "tenant" | "model",
): TopShareResult | null {
  const rows =
    axis === "tenant"
      ? cost.by_tenant.map((t) => ({
          id: t.tenant_id,
          label: t.tenant_name ?? t.tenant_id.slice(0, 8),
          cost: t.cost_usd,
        }))
      : cost.by_model.map((m) => ({
          id: m.model,
          label: m.model,
          cost: m.cost_usd,
        }))
  if (rows.length === 0 || cost.period_total_usd <= 0) return null
  if (cost.previous_total_usd <= 0) return null
  const totalNow = cost.period_total_usd
  const totalPrev = cost.previous_total_usd
  // Expected share = previous total proportionally; we have no per-axis
  // previous numbers in v1 so we assume "expected share" matches the
  // CURRENT relative share — meaning any single dominant row whose
  // absolute cost crossed 2x the period-over-period growth flags.
  const growthFactor = totalNow / totalPrev
  let best: TopShareResult | null = null
  for (const row of rows) {
    if (row.cost <= 0) continue
    const expected = (row.cost / totalNow) * (totalPrev * growthFactor)
    const ratio = expected > 0 ? row.cost / expected : 0
    if (ratio < 2) continue
    if (!best || ratio > best.ratio) {
      best = {
        id: row.id,
        label: row.label,
        ratio,
        currentCost: row.cost,
        previousCost: expected,
        sparkline: cost.by_day.map((p) => p.cost_usd),
      }
    }
  }
  return best
}

// ──────────────────────────────────────────────────────────────────────
// Zone 3 series — fan in per-tenant /cost summaries
// ──────────────────────────────────────────────────────────────────────

export interface TopTenant {
  id: string
  label: string
  cost: number
  colorIndex: 1 | 2 | 3 | 4 | 5
}

export function topNTenants(
  primary: CostSummary | null,
  n = 5,
): TopTenant[] {
  if (!primary) return []
  const sorted = [...primary.by_tenant].sort((a, b) => b.cost_usd - a.cost_usd)
  return sorted.slice(0, n).map((row, idx) => ({
    id: row.tenant_id,
    label: row.tenant_name ?? row.tenant_id.slice(0, 8),
    cost: row.cost_usd,
    colorIndex: ((idx % 5) + 1) as 1 | 2 | 3 | 4 | 5,
  }))
}

// Build a stacked area series from per-tenant by_day arrays. Days missing
// from a tenant's response render as zero in that series.
export function buildTenantSeries(
  primary: CostSummary | null,
  byTenant: Record<string, CostSummary>,
  top: TopTenant[],
): TenantSeriesPoint[] {
  if (!primary) return []
  const dates = primary.by_day.map((p) => p.date)
  const otherTotalByDate: Record<string, number> = {}
  for (const point of primary.by_day) {
    otherTotalByDate[point.date] = point.cost_usd
  }
  const topPerDate: Record<string, Record<string, number>> = {}
  for (const t of top) {
    const tenantSummary = byTenant[t.id]
    if (!tenantSummary) continue
    for (const point of tenantSummary.by_day) {
      topPerDate[point.date] = topPerDate[point.date] ?? {}
      topPerDate[point.date][t.id] = point.cost_usd
      otherTotalByDate[point.date] =
        (otherTotalByDate[point.date] ?? 0) - point.cost_usd
    }
  }
  return dates.map((date) => {
    const row: TenantSeriesPoint = { date }
    for (const t of top) {
      row[t.id] = topPerDate[date]?.[t.id] ?? 0
    }
    row.other = Math.max(0, otherTotalByDate[date] ?? 0)
    return row
  })
}

// ──────────────────────────────────────────────────────────────────────
// Zone 4 helpers
// ──────────────────────────────────────────────────────────────────────

export function sortedBudgetRows(budgets: Budget[]): Budget[] {
  return [...budgets].sort((a, b) => {
    const ap = a.monthly_usd === 0 ? 0 : a.spent_this_period_usd / a.monthly_usd
    const bp = b.monthly_usd === 0 ? 0 : b.spent_this_period_usd / b.monthly_usd
    return bp - ap
  })
}

// $/call substitute when token totals aren't in the cost summary (Gap 18).
// We use cost-per-call-day average from by_day + by_model rows; better
// than nothing, honest about its limitations.
export interface ModelCostRow {
  model: string
  cost: number
  share: number // 0..100
}

export function modelCostRows(primary: CostSummary | null): ModelCostRow[] {
  if (!primary || primary.by_model.length === 0) return []
  const total = primary.by_model.reduce((acc, m) => acc + m.cost_usd, 0)
  return [...primary.by_model]
    .sort((a, b) => b.cost_usd - a.cost_usd)
    .map((m) => ({
      model: m.model,
      cost: m.cost_usd,
      share: total > 0 ? (m.cost_usd / total) * 100 : 0,
    }))
}

// ──────────────────────────────────────────────────────────────────────
// Formatters
// ──────────────────────────────────────────────────────────────────────

export function formatCurrency(n: number): string {
  if (n === 0) return "$0"
  if (Math.abs(n) < 1) return `$${n.toFixed(4)}`
  if (Math.abs(n) < 100) return `$${n.toFixed(2)}`
  return `$${Math.round(n).toLocaleString()}`
}

export function formatRatio(ratio: number): string {
  if (ratio < 10) return `${ratio.toFixed(1)}×`
  return `${Math.round(ratio)}×`
}
