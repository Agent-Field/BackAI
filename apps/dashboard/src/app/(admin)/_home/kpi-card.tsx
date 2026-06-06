"use client"

// KPI tile: label + tabular value + 24-point sparkline.
//
// Pure presentation. Data shape is a flat number array (24 hourly points
// from the `home.*_sparkline` fields). Renders cleanly with an empty
// array — the area just stays at zero baseline.

import { Area, AreaChart } from "recharts"
import type { LucideIcon } from "lucide-react"

import { ChartContainer, type ChartConfig } from "@/components/ui/chart"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"

const sparkConfig = {
  value: {
    label: "Value",
    color: "var(--chart-1)",
  },
} satisfies ChartConfig

type KpiCardProps = {
  label: string
  value: string
  description?: string
  icon: LucideIcon
  sparkline: number[]
}

export function KpiCard({
  label,
  value,
  description,
  icon: Icon,
  sparkline,
}: KpiCardProps) {
  const data = (sparkline ?? []).map((v, i) => ({ i, value: v }))
  const hasData = data.some((d) => d.value > 0)

  return (
    <Card>
      <CardHeader>
        <CardDescription className="flex items-center gap-2">
          <Icon className="size-4" />
          {label}
        </CardDescription>
        <CardTitle className="text-3xl tabular-nums">{value}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        <ChartContainer
          config={sparkConfig}
          className="aspect-auto h-12 w-full"
        >
          <AreaChart
            data={data.length > 0 ? data : [{ i: 0, value: 0 }, { i: 1, value: 0 }]}
            margin={{ top: 2, right: 0, left: 0, bottom: 0 }}
          >
            <defs>
              <linearGradient id="kpi-spark-fill" x1="0" y1="0" x2="0" y2="1">
                <stop
                  offset="0%"
                  stopColor="var(--color-value)"
                  stopOpacity={0.35}
                />
                <stop
                  offset="100%"
                  stopColor="var(--color-value)"
                  stopOpacity={0}
                />
              </linearGradient>
            </defs>
            <Area
              type="monotone"
              dataKey="value"
              stroke="var(--color-value)"
              strokeWidth={1.5}
              fill="url(#kpi-spark-fill)"
              isAnimationActive={false}
            />
          </AreaChart>
        </ChartContainer>
        {description ? (
          <p className="text-muted-foreground text-xs">
            {hasData ? description : "No traffic in the last 24 hours."}
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}
