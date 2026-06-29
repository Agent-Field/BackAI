// SPDX-License-Identifier: Apache-2.0

"use client"

import { ArrowDownRight, ArrowRight, ArrowUpRight } from "lucide-react"

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"

import type { KpiTileModel, StatusState } from "@/lib/home/types"

import { KpiSparkline } from "./kpi-sparkline"

// One tile in the KPI strip.
//
// Layout is vertical so the sparkline always gets the tile's full width
// — fixing the prior version's horizontal-flex overflow where sparkline
// pixels leaked across the divide-x dividers into adjacent cells.
//
// Rows top-to-bottom:
//   1. dot · LABEL  · (optional state chip right-aligned)
//   2. VALUE        (text-kpi-value, semibold, tabular-nums)
//   3. delta arrow + percent (or "flat" / "—")
//   4. sparkline    (full container width, fixed pixel height)
//   5. sub-label    (optional muted text)

const DOT_CLASS: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/40",
}

const TILE_HELP: Partial<Record<KpiTileModel["id"], string>> = {
  "live-runs":
    "Background jobs currently executing. For sync traffic see Requests/min.",
  "budget-consumed":
    "Average % of monthly budget consumed across tenants with a budget set.",
  "failed-24h":
    "Agent invocations that returned non-2xx in the last 24h.",
  "error-rate":
    "Share of gateway requests in the last 60m returning non-2xx.",
}

export interface KpiTileProps {
  tile: KpiTileModel
  goodDirection?: "up" | "down" | "neutral"
}

export function KpiTile({ tile, goodDirection = "neutral" }: KpiTileProps) {
  const formatted = formatValue(tile)
  const stateBadge = stateBadgeText(tile.state)
  const help = TILE_HELP[tile.id]
  return (
    <div className="flex h-full min-w-0 flex-col gap-tile-tight px-tile py-tile">
      {/* Row 1: dot + label + (optional state chip) */}
      <div className="flex min-w-0 items-center gap-inline">
        <span
          aria-hidden
          className={`inline-block size-icon-dot shrink-0 rounded-pill ${DOT_CLASS[tile.status]}`}
        />
        {help ? (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  className="min-w-0 cursor-help truncate bg-transparent p-0 text-left text-eyebrow font-medium uppercase tracking-wide text-muted-foreground"
                />
              }
            >
              {tile.label}
            </TooltipTrigger>
            <TooltipContent>{help}</TooltipContent>
          </Tooltip>
        ) : (
          <span className="min-w-0 truncate text-eyebrow font-medium uppercase tracking-wide text-muted-foreground">
            {tile.label}
          </span>
        )}
        {stateBadge ? (
          <span className="ml-auto shrink-0 rounded-md border px-1.5 py-0.5 text-meta text-muted-foreground">
            {stateBadge}
          </span>
        ) : null}
      </div>

      {/* Row 2: value */}
      <span className="truncate text-kpi-value font-semibold tabular-nums text-foreground">
        {formatted}
      </span>

      {/* Row 3: delta */}
      <DeltaBadge deltaPct={tile.deltaPct} goodDirection={goodDirection} />

      {/* Row 4: sparkline — full container width */}
      <KpiSparkline data={tile.sparkline} status={tile.status} />

      {/* Row 5: sub-label */}
      {tile.subLabel ? (
        <span className="truncate text-meta text-muted-foreground">
          {tile.subLabel}
        </span>
      ) : null}
    </div>
  )
}

function DeltaBadge({
  deltaPct,
  goodDirection,
}: {
  deltaPct: number | null
  goodDirection: "up" | "down" | "neutral"
}) {
  if (deltaPct === null || !Number.isFinite(deltaPct)) {
    return (
      <span className="inline-flex items-center gap-0.5 text-meta text-muted-foreground">
        —
      </span>
    )
  }
  const abs = Math.abs(deltaPct)
  if (abs < 0.5) {
    return (
      <span className="inline-flex items-center gap-0.5 text-meta text-muted-foreground">
        <ArrowRight className="size-3" aria-hidden />
        flat
      </span>
    )
  }
  const up = deltaPct > 0
  const Icon = up ? ArrowUpRight : ArrowDownRight
  let toneClass = "text-muted-foreground"
  if (goodDirection !== "neutral") {
    const directionMatchesGood =
      (goodDirection === "up" && up) || (goodDirection === "down" && !up)
    if (directionMatchesGood) {
      toneClass = "text-success"
    } else if (abs >= 25) {
      toneClass = "text-destructive"
    } else {
      toneClass = "text-warning"
    }
  }
  return (
    <span className={`inline-flex items-center gap-0.5 text-meta ${toneClass}`}>
      <Icon className="size-3" aria-hidden />
      {formatDelta(abs)}%
    </span>
  )
}

function formatDelta(abs: number): string {
  if (abs >= 100) return Math.round(abs).toString()
  if (abs >= 10) return abs.toFixed(0)
  return abs.toFixed(1)
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
      return Math.round(tile.value).toString()
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
  if (n === 0) return "$0"
  if (n < 1) return `$${n.toFixed(4)}`
  if (n < 100) return `$${n.toFixed(2)}`
  return `$${Math.round(n).toLocaleString()}`
}
