// SPDX-License-Identifier: Apache-2.0

"use client"

import { Line, LineChart, ResponsiveContainer } from "recharts"

import { chart } from "@/lib/theme"
import type { StatusState } from "@/lib/home/types"

// Minimal monochrome (or status-tinted) sparkline. Sized to fill its
// container's full width with a fixed pixel height. The previous
// horizontal-flex layout overflowed into adjacent tiles because the
// chart's intrinsic width fought a fixed-width sibling; the v3 KPI tile
// is vertical-stack, so the sparkline owns its row and uses
// ResponsiveContainer at width 100% with overflow hidden on the parent.

const STROKE: Record<StatusState, string> = {
  ok: "var(--color-foreground)",
  watch: "var(--color-warning)",
  act: "var(--color-destructive)",
  idle: "var(--color-muted-foreground)",
}

interface KpiSparklineProps {
  data: number[]
  status: StatusState
}

export function KpiSparkline({ data, status }: KpiSparklineProps) {
  const meaningful = data.length > 0 && data.some((v) => v > 0)
  if (!meaningful) {
    // Ghost baseline so layout stays consistent across tiles that have
    // a sparkline and tiles that don't.
    return (
      <div
        aria-hidden
        className="w-full border-b border-dashed border-muted-foreground/30"
        style={{ height: chart.sparklineHeight }}
      />
    )
  }
  const series = data.map((v, idx) => ({ idx, v }))
  return (
    <div
      className="w-full overflow-hidden"
      style={{ height: chart.sparklineHeight }}
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
            strokeWidth={chart.sparklineStroke}
            dot={false}
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
