// Top-of-page metrics: period total, delta vs previous period, forecast
// for the current period, and (optionally) budget consumption.

import { ArrowDown, ArrowRight, ArrowUp } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import type { CostSummary } from "@/lib/api"

import { formatCurrency, formatCurrencyCompact, formatPercentDelta } from "./format"

type CostHeaderProps = {
  data: CostSummary
}

const DIRECTION_VARIANT: Record<
  "up" | "down" | "flat",
  "default" | "destructive" | "secondary" | "outline"
> = {
  // Cost going UP is bad (destructive). Going down is good (default).
  up: "destructive",
  down: "default",
  flat: "outline",
}

const DIRECTION_ICON = {
  up: ArrowUp,
  down: ArrowDown,
  flat: ArrowRight,
} as const

export function CostHeader({ data }: CostHeaderProps) {
  const delta = formatPercentDelta(data.period_total_usd, data.previous_total_usd)
  const DeltaIcon = DIRECTION_ICON[delta.direction]
  const budgetPct =
    data.budget_usd && data.budget_usd > 0
      ? Math.min(100, (data.period_total_usd / data.budget_usd) * 100)
      : null

  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
      <Card>
        <CardHeader>
          <CardDescription>Period total</CardDescription>
          <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
            {formatCurrency(data.period_total_usd)}
          </CardTitle>
          <div className="mt-2 flex items-center gap-2">
            <Badge variant={DIRECTION_VARIANT[delta.direction]}>
              <DeltaIcon />
              {delta.text}
            </Badge>
            <span className="text-muted-foreground text-xs">
              vs previous period
            </span>
          </div>
        </CardHeader>
      </Card>

      <Card>
        <CardHeader>
          <CardDescription>Forecast</CardDescription>
          <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
            {formatCurrencyCompact(data.forecast_usd)}
          </CardTitle>
          <p className="text-muted-foreground mt-2 text-xs">
            Projected spend through end of period at current run-rate.
          </p>
        </CardHeader>
      </Card>

      <Card>
        <CardHeader>
          <CardDescription>Budget</CardDescription>
          {budgetPct === null ? (
            <>
              <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
                —
              </CardTitle>
              <p className="text-muted-foreground mt-2 text-xs">
                No budget configured for this period.
              </p>
            </>
          ) : (
            <>
              <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
                {formatCurrencyCompact(data.budget_usd ?? 0)}
              </CardTitle>
              <div className="mt-3 flex items-center justify-between gap-2 text-xs">
                <span className="text-muted-foreground">
                  {budgetPct.toFixed(0)}% used
                </span>
                <span className="text-muted-foreground tabular-nums">
                  {formatCurrencyCompact(data.period_total_usd)} /{" "}
                  {formatCurrencyCompact(data.budget_usd ?? 0)}
                </span>
              </div>
              <Progress value={budgetPct} className="mt-2 w-full" />
            </>
          )}
        </CardHeader>
      </Card>
    </div>
  )
}
