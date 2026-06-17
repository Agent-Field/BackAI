// SPDX-License-Identifier: Apache-2.0

import { Check } from "lucide-react"

import { DeltaIndicator } from "@/components/ui/delta-indicator"
import { ForecastBar } from "@/components/ui/forecast-bar"
import { Sparkline } from "@/components/ui/sparkline"

import type { CostSummary } from "@/lib/api"
import { deriveAnomalies, formatCurrency } from "@/lib/cost/derive"
import type { CostSnapshot } from "@/lib/cost/types"

import { AnomalyCard } from "./anomaly-card"
import { ZoneCard, ZoneCardHeader } from "./zone-card"

// Zone 1 — Anomaly Strip. Primary read for the Cost page.
//
//   ┌──────────────────────┐  ┌──────────────────────────────┐
//   │  PERIOD TOTAL tile   │  │ FORECAST tile                │
//   └──────────────────────┘  └──────────────────────────────┘
//   ┌── anomaly cards (or affirmative all-clear) ────────────┐
//   └────────────────────────────────────────────────────────┘

export function Zone1AnomalyStrip({
  snapshot,
}: {
  snapshot: CostSnapshot
}) {
  const anomalies = deriveAnomalies(snapshot)
  const sparkline = snapshot.primary?.by_day.map((p) => p.cost_usd) ?? []
  return (
    <ZoneCard aria-labelledby="zone1-heading">
      <ZoneCardHeader
        id="zone1-heading"
        title="At a glance"
        subtitle={
          anomalies.length === 0
            ? "Nothing unusual."
            : `${anomalies.length} item${anomalies.length === 1 ? "" : "s"} to look at`
        }
      />
      <div className="grid gap-stack p-row-x md:grid-cols-2">
        <TodayTile primary={snapshot.primary} />
        <ForecastTile primary={snapshot.primary} />
      </div>
      <div className="border-t bg-card/40 p-row-x">
        {anomalies.length > 0 ? (
          <div className="flex flex-col gap-stack">
            {anomalies.map((anomaly) => (
              <AnomalyCard key={anomaly.id} anomaly={anomaly} />
            ))}
          </div>
        ) : (
          <AllClearCard sparkline={sparkline} />
        )}
      </div>
    </ZoneCard>
  )
}

// Affirmative empty state for Zone 1. The brief calls for this to be a
// positive card, not a footnote — operators should feel GOOD when
// nothing is unusual.
function AllClearCard({ sparkline }: { sparkline: number[] }) {
  return (
    <article className="flex flex-col gap-tile rounded-md border border-l-4 border-l-success bg-card px-row-x py-tile">
      <header className="flex items-start gap-stack">
        <Check
          className="mt-0.5 size-icon-inline shrink-0 text-success"
          aria-hidden
        />
        <div className="flex min-w-0 flex-1 flex-col gap-tile-tight">
          <h3 className="text-body font-medium text-foreground">
            Nothing unusual
          </h3>
          <p className="text-meta text-muted-foreground">
            All tenants, agents, and models within expected ranges.
          </p>
        </div>
      </header>
      <Sparkline data={sparkline} status="ok" height={32} />
      <span className="text-meta text-muted-foreground">
        Spend shape across the selected range.
      </span>
    </article>
  )
}

function TodayTile({ primary }: { primary: CostSummary | null }) {
  const total = primary?.period_total_usd ?? 0
  const prev = primary?.previous_total_usd ?? 0
  const sparkline = primary?.by_day.map((p) => p.cost_usd) ?? []
  return (
    <div className="flex flex-col gap-tile rounded-md border bg-card px-row-x py-tile">
      <div className="flex items-baseline justify-between">
        <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
          Period total
        </span>
        {prev > 0 ? (
          <DeltaIndicator
            current={total}
            previous={prev}
            format="currency"
            semantic="cost"
          />
        ) : null}
      </div>
      <span className="text-2xl font-semibold tabular-nums text-foreground">
        {formatCurrency(total)}
      </span>
      <Sparkline data={sparkline} status="ok" height={32} />
      <span className="text-meta text-muted-foreground">
        {prev > 0
          ? `vs ${formatCurrency(prev)} in the prior period`
          : "First period of data."}
      </span>
    </div>
  )
}

function ForecastTile({ primary }: { primary: CostSummary | null }) {
  const spent = primary?.period_total_usd ?? 0
  const projected = primary?.forecast_usd ?? spent
  const budget = primary?.budget_usd ?? null
  const overBudget = budget !== null && projected > budget
  const sublabel =
    budget === null
      ? `${formatCurrency(spent)} spent · ${formatCurrency(projected)} projected · no budget set`
      : overBudget
        ? `${formatCurrency(projected)} projected vs ${formatCurrency(budget)} budget — over by ${formatCurrency(projected - budget)}`
        : `${formatCurrency(spent)} spent · ${formatCurrency(projected)} projected vs ${formatCurrency(budget)} budget`
  return (
    <div className="flex flex-col gap-tile rounded-md border bg-card px-row-x py-tile">
      <div className="flex items-baseline justify-between">
        <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
          Forecast
        </span>
        <span className="text-meta tabular-nums text-muted-foreground">
          {budget === null
            ? "—"
            : `${Math.round((projected / budget) * 100)}% of budget`}
        </span>
      </div>
      <span className="text-2xl font-semibold tabular-nums text-foreground">
        {formatCurrency(projected)}
      </span>
      <ForecastBar spent={spent} projected={projected} budget={budget} />
      <span className="text-meta text-muted-foreground">{sublabel}</span>
    </div>
  )
}
