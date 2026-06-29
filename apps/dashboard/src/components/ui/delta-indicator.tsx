// SPDX-License-Identifier: Apache-2.0

import { ArrowDown, ArrowUp, Minus } from "lucide-react"

import { cn } from "@/lib/utils"

// DeltaIndicator — directional change between two values, formatted with
// semantics. "Cost" semantics flip the colour expectation: a rising
// cost number is bad (destructive), a falling one is good (success).
//
// All colour comes from semantic tokens. No raw hex.

type DeltaFormat = "percent" | "currency" | "number"
type DeltaSemantic = "cost" | "revenue" | "neutral"

interface DeltaIndicatorProps {
  current: number
  previous: number
  format?: DeltaFormat
  semantic?: DeltaSemantic
  /** Show the absolute delta alongside the percent: e.g. `▴$8.18 (18%)`. */
  showAbsolute?: boolean
  className?: string
}

export function DeltaIndicator({
  current,
  previous,
  format = "percent",
  semantic = "neutral",
  showAbsolute = false,
  className,
}: DeltaIndicatorProps) {
  const absolute = current - previous
  const pct = previous === 0 ? null : (absolute / previous) * 100
  const direction: "up" | "down" | "flat" = (() => {
    if (absolute > 0) return "up"
    if (absolute < 0) return "down"
    return "flat"
  })()

  const colour = colourFor(direction, semantic)
  const Icon = ICON[direction]
  const pctLabel = pct === null ? "new" : formatPct(pct)
  const absLabel = formatAbs(absolute, format)

  return (
    <span
      className={cn(
        "inline-flex items-center gap-inline text-meta tabular-nums font-mono",
        colour,
        className,
      )}
    >
      <Icon className="size-3" aria-hidden />
      {showAbsolute ? (
        <span>
          {absLabel} ({pctLabel})
        </span>
      ) : (
        <span>{pctLabel}</span>
      )}
    </span>
  )
}

const ICON = {
  up: ArrowUp,
  down: ArrowDown,
  flat: Minus,
} as const

function colourFor(
  direction: "up" | "down" | "flat",
  semantic: DeltaSemantic,
): string {
  if (direction === "flat") return "text-muted-foreground"
  const goodIsUp = semantic === "revenue"
  const goodIsDown = semantic === "cost"
  const good =
    (direction === "up" && goodIsUp) || (direction === "down" && goodIsDown)
  const bad =
    (direction === "up" && goodIsDown) || (direction === "down" && goodIsUp)
  if (good) return "text-success"
  if (bad) return "text-destructive"
  return "text-muted-foreground"
}

function formatPct(pct: number): string {
  const sign = pct >= 0 ? "" : "-"
  const abs = Math.abs(pct)
  // Very large deltas read better as multipliers than percentages.
  if (abs >= 200) {
    const x = abs / 100
    return `${sign}${x.toFixed(x < 10 ? 1 : 0)}×`
  }
  if (abs < 10) return `${sign}${abs.toFixed(1)}%`
  return `${sign}${Math.round(abs)}%`
}

function formatAbs(value: number, format: DeltaFormat): string {
  const sign = value >= 0 ? "" : "-"
  const abs = Math.abs(value)
  switch (format) {
    case "currency":
      if (abs < 1) return `${sign}$${abs.toFixed(4)}`
      if (abs < 100) return `${sign}$${abs.toFixed(2)}`
      return `${sign}$${Math.round(abs).toLocaleString()}`
    case "number":
      return `${sign}${abs.toLocaleString()}`
    case "percent":
      return `${sign}${abs.toFixed(1)}pp`
  }
}
