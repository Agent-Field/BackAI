// SPDX-License-Identifier: Apache-2.0

"use client"

import { Badge } from "@/components/ui/badge"
import { Card, CardContent } from "@/components/ui/card"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"

import type { KpiTileModel, StatusState } from "@/lib/home/types"
import { KpiSparkline } from "./kpi-sparkline"

// One tile in the KPI strip. Renders identical structure regardless of
// data state — only the value / sparkline / chips swap — so the page's
// height never shifts as data arrives. This is the home.md §10 contract.

const STATUS_DOT: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/40",
}

const TILE_HELP: Partial<Record<KpiTileModel["id"], string>> = {
  "live-runs":
    "Background jobs currently being processed. For sync traffic, see Requests/min.",
  "budget-consumed":
    "Average percentage of monthly budget consumed across all tenants with a budget set.",
  "failed-24h":
    "Agent invocations that returned a non-2xx response in the last 24 hours.",
  "error-rate":
    "Share of gateway requests in the last 60 minutes that returned a non-2xx status.",
}

export function KpiTile({ tile }: { tile: KpiTileModel }) {
  const formatted = formatValue(tile)
  const stateBadge = stateBadgeText(tile.state)
  const help = TILE_HELP[tile.id]
  return (
    <Card className="gap-tile-tight">
      <CardContent className="flex flex-col gap-tile-tight">
        <div className="flex items-center gap-inline">
          <span
            aria-hidden
            className={`inline-block h-2 w-2 rounded-pill ${STATUS_DOT[tile.status]}`}
          />
          {help ? (
            <Tooltip>
              <TooltipTrigger className="text-eyebrow uppercase text-muted-foreground cursor-help bg-transparent border-0 p-0 text-left">
                {tile.label}
              </TooltipTrigger>
              <TooltipContent>{help}</TooltipContent>
            </Tooltip>
          ) : (
            <span className="text-eyebrow uppercase text-muted-foreground">
              {tile.label}
            </span>
          )}
          {stateBadge ? (
            <Badge variant="outline" className="ml-auto text-meta">
              {stateBadge}
            </Badge>
          ) : null}
        </div>
        <div className="text-kpi-value font-semibold tabular-nums text-foreground">
          {formatted}
        </div>
        {tile.subLabel ? (
          <div className="text-meta text-muted-foreground">{tile.subLabel}</div>
        ) : null}
        <KpiSparkline data={tile.sparkline} />
      </CardContent>
    </Card>
  )
}

function stateBadgeText(state: KpiTileModel["state"]): string | null {
  switch (state) {
    case "live":
      return null
    case "empty":
      return "empty"
    case "missing":
      return "n/a"
    case "degraded":
      return "stale"
  }
}

function formatValue(tile: KpiTileModel): string {
  if (tile.value === null) return "—"
  switch (tile.unit) {
    case "count":
      return formatCount(tile.value)
    case "rate":
      return tile.value.toFixed(1)
    case "percent":
      return `${(tile.value * 100).toFixed(1)}%`
    case "percent-as-whole":
      return `${tile.value.toFixed(0)}%`
    case "currency-usd":
      return formatUSD(tile.value)
  }
}

function formatCount(n: number): string {
  if (n >= 1000) {
    const k = n / 1000
    return `${k.toFixed(k >= 10 ? 0 : 1)}k`
  }
  return Math.round(n).toLocaleString()
}

function formatUSD(n: number): string {
  if (n < 1) return `$${n.toFixed(4)}`
  if (n < 100) return `$${n.toFixed(2)}`
  return `$${Math.round(n).toLocaleString()}`
}
