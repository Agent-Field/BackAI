"use client"

// Daily spend chart. Stacked area when broken out by model, single area
// otherwise. Renders cleanly with an empty `by_day` array (just an empty
// chart frame).

import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts"
import { useMemo } from "react"

import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import type { CostSummary } from "@/lib/api"
import { CircleDollarSign } from "lucide-react"

const CHART_COLORS = [
  "var(--chart-1)",
  "var(--chart-2)",
  "var(--chart-3)",
  "var(--chart-4)",
  "var(--chart-5)",
]

const TOTAL_KEY = "total"

type CostAreaChartProps = {
  data: CostSummary
  /** Optional: top N models to stack. Defaults to 5. */
  topN?: number
}

function formatDayLabel(iso: string): string {
  // ISO date → "Jun 03". Avoid timezone shifts by parsing as local
  // date-only.
  const [y, m, d] = iso.slice(0, 10).split("-").map((n) => parseInt(n, 10))
  if (!y || !m || !d) return iso
  const date = new Date(y, m - 1, d)
  return date.toLocaleDateString("en-US", { month: "short", day: "2-digit" })
}

function formatCurrencyTick(usd: number): string {
  if (usd === 0) return "$0"
  if (Math.abs(usd) < 1) return `$${usd.toFixed(2)}`
  if (Math.abs(usd) < 1000) return `$${usd.toFixed(0)}`
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(usd)
}

export function CostAreaChart({ data, topN = 5 }: CostAreaChartProps) {
  const { rows, config, modelKeys } = useMemo(() => {
    const topModels = [...data.by_model]
      .sort((a, b) => b.cost_usd - a.cost_usd)
      .slice(0, topN)
      .map((m) => m.model)

    // Distribute the per-day total across top models proportionally to
    // their overall share. Without a real per-day-per-model series, this
    // is the right approximation: it preserves the daily total and the
    // model ranking.
    const totalCost = data.by_model.reduce((acc, m) => acc + m.cost_usd, 0)
    const share = (model: string): number => {
      if (totalCost <= 0) return 0
      const m = data.by_model.find((x) => x.model === model)
      return m ? m.cost_usd / totalCost : 0
    }
    const otherShare = topModels.length
      ? 1 - topModels.reduce((acc, m) => acc + share(m), 0)
      : 1

    const rows = data.by_day.map((point) => {
      const row: Record<string, number | string> = {
        date: point.date,
        [TOTAL_KEY]: point.cost_usd,
      }
      for (const model of topModels) {
        row[model] = point.cost_usd * share(model)
      }
      if (topModels.length && otherShare > 0.001) {
        row["__other__"] = point.cost_usd * otherShare
      }
      return row
    })

    const modelKeys =
      topModels.length === 0
        ? [TOTAL_KEY]
        : otherShare > 0.001
          ? [...topModels, "__other__"]
          : topModels

    const config: ChartConfig = {}
    modelKeys.forEach((key, idx) => {
      config[key] = {
        label: key === "__other__" ? "Other" : key === TOTAL_KEY ? "Spend" : key,
        color: CHART_COLORS[idx % CHART_COLORS.length],
      }
    })

    return { rows, config, modelKeys }
  }, [data, topN])

  const hasData = rows.length > 0 && rows.some((r) => Number(r[TOTAL_KEY]) > 0)

  return (
    <Card>
      <CardHeader>
        <CardTitle>Daily spend</CardTitle>
        <CardDescription>
          {modelKeys.length > 1
            ? "Stacked by top models for the selected range."
            : "Total spend per day for the selected range."}
        </CardDescription>
      </CardHeader>
      <div className="px-4 pb-4">
        {!hasData ? (
          <Empty className="border-dashed">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <CircleDollarSign />
              </EmptyMedia>
              <EmptyTitle>No spend in this range</EmptyTitle>
              <EmptyDescription>
                Costs appear here once an agent invokes a billed model.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <ChartContainer config={config} className="aspect-auto h-[320px] w-full">
            <AreaChart data={rows} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
              <defs>
                {modelKeys.map((key) => (
                  <linearGradient
                    key={key}
                    id={`cost-fill-${key}`}
                    x1="0"
                    y1="0"
                    x2="0"
                    y2="1"
                  >
                    <stop
                      offset="0%"
                      stopColor={`var(--color-${key})`}
                      stopOpacity={0.4}
                    />
                    <stop
                      offset="100%"
                      stopColor={`var(--color-${key})`}
                      stopOpacity={0}
                    />
                  </linearGradient>
                ))}
              </defs>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis
                dataKey="date"
                tickLine={false}
                axisLine={false}
                minTickGap={32}
                tickFormatter={formatDayLabel}
              />
              <YAxis
                tickLine={false}
                axisLine={false}
                width={56}
                tickFormatter={formatCurrencyTick}
              />
              <ChartTooltip
                cursor={false}
                content={
                  <ChartTooltipContent
                    indicator="dot"
                    labelFormatter={(label) =>
                      typeof label === "string" ? formatDayLabel(label) : String(label)
                    }
                    formatter={(value, name) => (
                      <span className="flex w-full items-center justify-between gap-3">
                        <span className="text-muted-foreground">
                          {config[String(name)]?.label ?? String(name)}
                        </span>
                        <span className="font-mono tabular-nums">
                          {formatCurrencyTick(Number(value))}
                        </span>
                      </span>
                    )}
                  />
                }
              />
              {modelKeys.length > 1 ? (
                <ChartLegend
                  content={<ChartLegendContent />}
                  verticalAlign="bottom"
                />
              ) : null}
              {modelKeys.map((key) => (
                <Area
                  key={key}
                  dataKey={key}
                  type="monotone"
                  stackId={modelKeys.length > 1 ? "models" : undefined}
                  stroke={`var(--color-${key})`}
                  fill={`url(#cost-fill-${key})`}
                  strokeWidth={1.5}
                  isAnimationActive={false}
                />
              ))}
            </AreaChart>
          </ChartContainer>
        )}
      </div>
    </Card>
  )
}
