// Top-of-page hero metric strip: 4 cards.
//
//   1. Period total spend, with vs-previous delta Badge
//   2. Budget remaining (Progress bar; "No budget set" if none configured)
//   3. Forecast for current period (linear extrapolation from server)
//   4. Cache hit rate, with savings_usd subline
//
// Cache stats are optional — they come from the LLM gateway and the
// endpoint may not exist yet on older runtimes. When `cache` is null
// we render a calm fallback state instead of a noisy error.

import { ArrowDown, ArrowRight, ArrowUp, Zap } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import type { CacheStats, CostSummary } from "@/lib/api"

import {
  formatCurrency,
  formatCurrencyCompact,
  formatPercentDelta,
} from "./format"

type CostHeaderProps = {
  data: CostSummary
  cache: CacheStats | null
  /**
   * Effective budget. The runtime exposes both a coarse `data.budget_usd`
   * (account-wide for the period) and the per-tenant `budgets` collection.
   * The page picks whichever is more relevant for the active filters and
   * passes it down here. `null` means "no budget set" — render an empty
   * card state instead of "—".
   */
  budgetUsd: number | null
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

export function CostHeader({ data, cache, budgetUsd }: CostHeaderProps) {
  const delta = formatPercentDelta(data.period_total_usd, data.previous_total_usd)
  const DeltaIcon = DIRECTION_ICON[delta.direction]
  const budgetPct =
    budgetUsd && budgetUsd > 0
      ? Math.min(100, (data.period_total_usd / budgetUsd) * 100)
      : null
  const cacheHitPct =
    cache && cache.total_calls > 0
      ? Math.round(cache.hit_rate * 100)
      : null

  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
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
          <CardDescription>Budget</CardDescription>
          {budgetPct === null ? (
            <>
              <CardTitle className="text-muted-foreground text-2xl font-medium tabular-nums tracking-tight">
                No budget set
              </CardTitle>
              <p className="text-muted-foreground mt-2 text-xs">
                Set a monthly budget to track remaining spend and trigger
                alerts.
              </p>
            </>
          ) : (
            <>
              <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
                {formatCurrencyCompact(
                  Math.max(0, (budgetUsd ?? 0) - data.period_total_usd),
                )}
              </CardTitle>
              <div className="mt-3 flex items-center justify-between gap-2 text-xs">
                <span className="text-muted-foreground">
                  {budgetPct.toFixed(0)}% used
                </span>
                <span className="text-muted-foreground tabular-nums">
                  {formatCurrencyCompact(data.period_total_usd)} /{" "}
                  {formatCurrencyCompact(budgetUsd ?? 0)}
                </span>
              </div>
              <Progress value={budgetPct} className="mt-2 w-full" />
            </>
          )}
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
          <CardDescription className="flex items-center gap-1.5">
            <Zap className="size-3" />
            Cache hit rate
          </CardDescription>
          {cacheHitPct === null ? (
            <>
              <CardTitle className="text-muted-foreground text-2xl font-medium tabular-nums tracking-tight">
                No cache yet
              </CardTitle>
              <p className="text-muted-foreground mt-2 text-xs">
                Cache stats appear once the LLM gateway serves a request.
              </p>
            </>
          ) : (
            <>
              <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
                {cacheHitPct}%
              </CardTitle>
              <p className="text-muted-foreground mt-2 text-xs">
                Saved {formatCurrencyCompact(cache?.savings_usd ?? 0)} across{" "}
                {(cache?.cache_hits ?? 0).toLocaleString()} cached calls.
              </p>
            </>
          )}
        </CardHeader>
      </Card>
    </div>
  )
}
