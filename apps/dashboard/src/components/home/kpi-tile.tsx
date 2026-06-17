// SPDX-License-Identifier: Apache-2.0

"use client"

import { ArrowDownRight, ArrowRight, ArrowUpRight } from "lucide-react"

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"

import { motion } from "@/lib/theme"
import type { KpiTileModel, StatusState } from "@/lib/home/types"
import { KpiSparkline } from "./kpi-sparkline"

// One tile in the KPI strip.
//
// Layout (one row, all signals visible at a glance):
//   [dot] LABEL                    [stale?]
//   VALUE          DELTA           [sparkline]
//
// Identical structure across all data states — only the value swaps to "—"
// and a stale chip appears when degraded. Density is the goal: tiles are
// thin horizontal cells, hairline-divided in the strip, no cards.

const DOT_CLASS: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/40",
}

const DELTA_HELP: Partial<Record<KpiTileModel["id"], string>> = {
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
  /**
   * Direction of "good": when an increasing value is desirable (e.g. RPM)
   * pass "up". When increase is bad (errors, cost, failures) pass "down".
   * Drives delta arrow colour.
   */
  goodDirection?: "up" | "down" | "neutral"
}

export function KpiTile({ tile, goodDirection = "neutral" }: KpiTileProps) {
  const formatted = formatValue(tile)
  const stateBadge = stateBadgeText(tile.state)
  const help = DELTA_HELP[tile.id]
  return (
    <div className="flex h-full flex-col justify-between gap-tile-tight px-tile py-tile">
      <div className="flex items-center gap-inline">
        <span
          aria-hidden
          className={`inline-block size-icon-dot rounded-pill ${DOT_CLASS[tile.status]}`}
        />
        {help ? (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  className="cursor-help bg-transparent p-0 text-left text-eyebrow font-medium uppercase tracking-wide text-muted-foreground"
                />
              }
            >
              {tile.label}
            </TooltipTrigger>
            <TooltipContent>{help}</TooltipContent>
          </Tooltip>
        ) : (
          <span className="text-eyebrow font-medium uppercase tracking-wide text-muted-foreground">
            {tile.label}
          </span>
        )}
        {stateBadge ? (
          <span className="ml-auto rounded-md border px-1.5 py-0.5 text-meta text-muted-foreground">
            {stateBadge}
          </span>
        ) : null}
      </div>
      <div className="flex items-end justify-between gap-inline">
        <div className="flex flex-col gap-0.5">
          <span
            className="text-kpi-value font-semibold tabular-nums text-foreground transition-opacity"
            style={{ transitionDuration: `${motion.tick}ms` }}
          >
            {formatted}
          </span>
          <DeltaBadge
            deltaPct={tile.deltaPct}
            goodDirection={goodDirection}
          />
        </div>
        <KpiSparkline data={tile.sparkline} status={tile.status} />
      </div>
      {tile.subLabel ? (
        <span className="text-meta text-muted-foreground">{tile.subLabel}</span>
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
    return <span className="text-meta text-muted-foreground">—</span>
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
  // Colour semantics: green when delta moves in the direction we want;
  // muted-yellow when neutral metric (e.g. cost — operator decides);
  // destructive only when delta moves in the bad direction by >25%.
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
