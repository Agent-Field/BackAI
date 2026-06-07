// Cost Explorer plugin page (Phase 12.3).
//
// Real first-party demo of a Plugin page. Mounted at
// `/plugins/cost-explorer` via the auto-generated route proxy under
// `src/app/(admin)/plugins/cost-explorer/page.tsx`.
//
// What this shows:
//   - Hero card with the period total + month-over-month delta
//   - Top-5 spenders by model (table)
//   - Top-5 spenders by tenant (table)
//   - A simple month-over-month delta bar comparing the current period
//     to the previous one (sub-cards)
//
// Everything degrades gracefully: when the runtime is unreachable or
// the cost aggregate is empty, the page renders a calm zero-state
// instead of erroring. Cost formatting reuses the shared helpers from
// `(admin)/operate/cost/_components/format.ts` so dollar values stay
// consistent across the dashboard.

import { ArrowDown, ArrowRight, ArrowUp, ServerCrash, TrendingUp } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import { Badge } from "@/components/ui/badge"
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
import { api, type CostSummary } from "@/lib/api"

import {
  formatCurrency,
  formatCurrencyCompact,
  formatPercentDelta,
} from "@/app/(admin)/operate/cost/_components/format"

export const dynamic = "force-dynamic"

const PERIOD_DAYS = 30

function isoFromDaysAgo(days: number): string {
  return new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString()
}

type Snapshot = {
  cost: CostSummary | null
}

async function loadSnapshot(): Promise<Snapshot> {
  const to = new Date().toISOString()
  const from = isoFromDaysAgo(PERIOD_DAYS)
  const result = await Promise.allSettled([api.cost({ from, to })])
  return {
    cost: result[0].status === "fulfilled" ? result[0].value : null,
  }
}

const DIRECTION_VARIANT: Record<
  "up" | "down" | "flat",
  "default" | "destructive" | "secondary" | "outline"
> = {
  up: "destructive",
  down: "default",
  flat: "outline",
}

const DIRECTION_ICON = {
  up: ArrowUp,
  down: ArrowDown,
  flat: ArrowRight,
} as const

type SpenderRow = { key: string; label: string; cost_usd: number }

function pickTop<T>(
  rows: T[],
  toRow: (row: T) => SpenderRow,
  limit = 5,
): SpenderRow[] {
  return rows
    .map(toRow)
    .filter((r) => r.cost_usd > 0)
    .sort((a, b) => b.cost_usd - a.cost_usd)
    .slice(0, limit)
}

function SpenderTable({
  title,
  description,
  rows,
  dimensionLabel,
}: {
  title: string
  description: string
  rows: SpenderRow[]
  dimensionLabel: string
}) {
  const total = rows.reduce((acc, r) => acc + r.cost_usd, 0)
  const max = rows[0]?.cost_usd ?? 0
  return (
    <Card className="gap-0">
      <CardHeader className="border-b pb-4">
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      {rows.length === 0 ? (
        <Empty className="border-0">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <TrendingUp />
            </EmptyMedia>
            <EmptyTitle>No spend by {dimensionLabel}</EmptyTitle>
            <EmptyDescription>
              Spend across this dimension appears here once usage is recorded.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="divide-y">
          {rows.map((row, idx) => {
            const sharePct = total > 0 ? (row.cost_usd / total) * 100 : 0
            const barPct = max > 0 ? (row.cost_usd / max) * 100 : 0
            return (
              <li
                key={row.key}
                className="grid grid-cols-[2rem_1fr_auto_auto] items-center gap-4 px-4 py-3 text-sm"
              >
                <div className="text-muted-foreground text-xs font-medium tabular-nums">
                  #{idx + 1}
                </div>
                <div className="min-w-0">
                  <div className="truncate font-medium" title={row.label}>
                    {row.label}
                  </div>
                  <div className="bg-muted mt-1.5 h-1 w-full overflow-hidden rounded-full">
                    <div
                      className="bg-primary h-full rounded-full transition-[width]"
                      style={{ width: `${barPct}%` }}
                      aria-hidden
                    />
                  </div>
                </div>
                <div className="text-muted-foreground w-12 text-right text-xs tabular-nums">
                  {sharePct.toFixed(1)}%
                </div>
                <div className="w-24 text-right font-mono text-sm tabular-nums">
                  {formatCurrency(row.cost_usd)}
                </div>
              </li>
            )
          })}
        </ul>
      )}
    </Card>
  )
}

function MoMDeltaCard({ data }: { data: CostSummary }) {
  const curr = data.period_total_usd
  const prev = data.previous_total_usd
  const max = Math.max(curr, prev, 1) // 1 avoids div-by-zero on zero-state
  const currPct = (curr / max) * 100
  const prevPct = (prev / max) * 100
  const diff = curr - prev
  const diffLabel =
    diff === 0
      ? "no change"
      : diff > 0
        ? `+${formatCurrency(diff)} more than previous period`
        : `${formatCurrency(diff)} less than previous period`

  return (
    <Card className="gap-0">
      <CardHeader className="border-b pb-4">
        <CardTitle>Month-over-month delta</CardTitle>
        <CardDescription>
          Comparing the current {PERIOD_DAYS}-day period to the previous one.
        </CardDescription>
      </CardHeader>
      <div className="flex flex-col gap-4 p-4">
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center justify-between text-sm">
            <span className="font-medium">This period</span>
            <span className="font-mono tabular-nums">
              {formatCurrency(curr)}
            </span>
          </div>
          <div className="bg-muted h-2 w-full overflow-hidden rounded-full">
            <div
              className="bg-primary h-full rounded-full transition-[width]"
              style={{ width: `${currPct}%` }}
              aria-hidden
            />
          </div>
        </div>
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground font-medium">
              Previous period
            </span>
            <span className="text-muted-foreground font-mono tabular-nums">
              {formatCurrency(prev)}
            </span>
          </div>
          <div className="bg-muted h-2 w-full overflow-hidden rounded-full">
            <div
              className="bg-muted-foreground/40 h-full rounded-full transition-[width]"
              style={{ width: `${prevPct}%` }}
              aria-hidden
            />
          </div>
        </div>
        <p className="text-muted-foreground text-xs">{diffLabel}</p>
      </div>
    </Card>
  )
}

export default async function CostExplorerPluginPage() {
  const { cost } = await loadSnapshot()

  if (!cost) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Cost Explorer"
          description="Top spenders and month-over-month delta."
        />
        <Empty className="border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ServerCrash />
            </EmptyMedia>
            <EmptyTitle>Runtime unreachable</EmptyTitle>
            <EmptyDescription>
              The dashboard could not reach the agent runtime. Spend metrics
              will appear here once the runtime is online.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  const delta = formatPercentDelta(
    cost.period_total_usd,
    cost.previous_total_usd,
  )
  const DeltaIcon = DIRECTION_ICON[delta.direction]

  const topModels = pickTop(cost.by_model, (m) => ({
    key: m.model,
    label: m.model,
    cost_usd: m.cost_usd,
  }))
  const topTenants = pickTop(cost.by_tenant, (t) => ({
    key: t.tenant_id,
    label: t.tenant_name ?? t.tenant_id,
    cost_usd: t.cost_usd,
  }))

  const totalIsZero = cost.period_total_usd === 0 && cost.by_day.length === 0

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Cost Explorer"
        description="Top spenders and month-over-month delta."
      />

      {/* Hero card: period total + delta vs previous period. */}
      <Card>
        <CardHeader>
          <CardDescription>
            Last {PERIOD_DAYS} days &middot; total spend
          </CardDescription>
          <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
            {formatCurrency(cost.period_total_usd)}
          </CardTitle>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <Badge variant={DIRECTION_VARIANT[delta.direction]}>
              <DeltaIcon className="size-3" />
              {delta.text}
            </Badge>
            <span className="text-muted-foreground text-sm">
              vs previous {PERIOD_DAYS} days
              {cost.previous_total_usd > 0
                ? ` (${formatCurrencyCompact(cost.previous_total_usd)})`
                : ""}
            </span>
          </div>
        </CardHeader>
      </Card>

      {totalIsZero ? (
        <Empty className="border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <TrendingUp />
            </EmptyMedia>
            <EmptyTitle>No spend recorded yet</EmptyTitle>
            <EmptyDescription>
              Cost tracking starts the moment an agent invokes a billed model.
              Run an agent to see top spenders here.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <>
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            <SpenderTable
              title="Top spenders by model"
              description="The 5 models accounting for the most spend in the period."
              rows={topModels}
              dimensionLabel="model"
            />
            <SpenderTable
              title="Top spenders by tenant"
              description="The 5 tenants accounting for the most spend in the period."
              rows={topTenants}
              dimensionLabel="tenant"
            />
          </div>
          <MoMDeltaCard data={cost} />
        </>
      )}
    </div>
  )
}
