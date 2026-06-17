// SPDX-License-Identifier: Apache-2.0

"use client"

import { Line, LineChart, ResponsiveContainer } from "recharts"

import { chart } from "@/lib/theme"
import type { StatusState } from "@/lib/home/types"

// Generic monochrome sparkline. Sized to fill its container's width with a
// fixed pixel height. Stroke colour follows the semantic StatusState so
// callers don't pass raw colour values — everything routes through the
// theme tokens declared in globals.css.
//
// Replaces the inlined sparkline in components/home/kpi-sparkline.tsx by
// moving the implementation here. The KPI tile keeps its thin wrapper for
// backwards-compatible naming.

const STROKE: Record<StatusState, string> = {
  ok: "var(--color-foreground)",
  watch: "var(--color-warning)",
  act: "var(--color-destructive)",
  idle: "var(--color-muted-foreground)",
}

export interface SparklineProps {
  data: number[]
  status?: StatusState
  /** Height in px. Defaults to the shared chart.sparklineHeight token. */
  height?: number
  /** Stroke width override. Defaults to chart.sparklineStroke. */
  strokeWidth?: number
  /** Optional aria-label. If omitted the sparkline is presentational. */
  ariaLabel?: string
}

export function Sparkline({
  data,
  status = "ok",
  height = chart.sparklineHeight,
  strokeWidth = chart.sparklineStroke,
  ariaLabel,
}: SparklineProps) {
  const meaningful = data.length > 0 && data.some((v) => v > 0)
  if (!meaningful) {
    return (
      <div
        aria-hidden={ariaLabel ? undefined : true}
        aria-label={ariaLabel}
        className="w-full border-b border-dashed border-muted-foreground/30"
        style={{ height }}
      />
    )
  }
  const series = data.map((v, idx) => ({ idx, v }))
  return (
    <div
      aria-hidden={ariaLabel ? undefined : true}
      aria-label={ariaLabel}
      role={ariaLabel ? "img" : undefined}
      className="w-full overflow-hidden"
      style={{ height }}
    >
      <ResponsiveContainer width="100%" height="100%">
        <LineChart
          data={series}
          margin={{ top: 2, right: 0, bottom: 2, left: 0 }}
        >
          <Line
            dataKey="v"
            type="monotone"
            stroke={STROKE[status]}
            strokeWidth={strokeWidth}
            dot={false}
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
