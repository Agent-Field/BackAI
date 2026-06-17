// SPDX-License-Identifier: Apache-2.0

import { cn } from "@/lib/utils"

import type { StatusState } from "@/lib/home/types"

// GaugeBar — single-segment progress with status-coloured fill. Distinct
// from ForecastBar (multi-segment "spent/projected/over"); GaugeBar is for
// "X of Y" reads where the colour communicates threshold state. Used in
// budgets table rows and reusable for rate-limit / quota strips.

interface GaugeBarProps {
  value: number
  max: number
  /** Status override; if omitted the bar derives state from value/max. */
  status?: StatusState
  /** Threshold for "watch" colour. Defaults to 70%. */
  watchAt?: number
  /** Threshold for "act" colour. Defaults to 90%. */
  actAt?: number
  className?: string
  ariaLabel?: string
}

const FILL: Record<StatusState, string> = {
  ok: "bg-success",
  watch: "bg-warning",
  act: "bg-destructive",
  idle: "bg-muted-foreground/40",
}

export function GaugeBar({
  value,
  max,
  status,
  watchAt = 70,
  actAt = 90,
  className,
  ariaLabel,
}: GaugeBarProps) {
  const pct = max <= 0 ? 0 : Math.max(0, Math.min(100, (value / max) * 100))
  const derived: StatusState =
    status ??
    (pct >= actAt ? "act" : pct >= watchAt ? "watch" : pct > 0 ? "ok" : "idle")
  return (
    <div
      role="img"
      aria-label={ariaLabel ?? `${pct.toFixed(0)}%`}
      className={cn(
        "relative w-full overflow-hidden rounded-pill bg-muted",
        className ?? "h-2",
      )}
    >
      <div
        className={cn(
          "absolute inset-y-0 left-0 transition-[width] duration-300",
          FILL[derived],
        )}
        style={{ width: `${pct}%` }}
      />
    </div>
  )
}
