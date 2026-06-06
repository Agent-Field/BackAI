// Cost dashboard — the differentiator tab vs "Supabase + Helicone
// separately." Server-renders the snapshot via `api.cost()` and falls
// back to a "runtime unreachable" empty state on error. Filters are
// driven by URL search params so navigation refetches the page; the
// filter bar itself is a client component below.

import { CircleDollarSign, ServerCrash } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { api, type CostSummary } from "@/lib/api"

import { CostAreaChart } from "./_components/cost-area-chart"
import { CostBreakdowns } from "./_components/cost-breakdowns"
import { CostFilters } from "./_components/cost-filters"
import { CostHeader } from "./_components/cost-header"

export const dynamic = "force-dynamic"

type SearchParams = Promise<{
  range?: string
  tenant?: string
  agent?: string
  model?: string
}>

const VALID_RANGES = new Set(["1d", "7d", "30d", "90d"])
const DEFAULT_RANGE = "30d"

function rangeToDays(range: string): number {
  switch (range) {
    case "1d":
      return 1
    case "7d":
      return 7
    case "90d":
      return 90
    default:
      return 30
  }
}

function isoFromDaysAgo(days: number): string {
  const now = Date.now()
  return new Date(now - days * 24 * 60 * 60 * 1000).toISOString()
}

async function loadCost(range: string): Promise<CostSummary | null> {
  try {
    const days = rangeToDays(range)
    return await api.cost({
      from: isoFromDaysAgo(days),
      to: new Date().toISOString(),
    })
  } catch {
    return null
  }
}

export default async function CostPage({
  searchParams,
}: {
  searchParams: SearchParams
}) {
  const params = await searchParams
  const range = VALID_RANGES.has(params.range ?? "")
    ? (params.range as string)
    : DEFAULT_RANGE
  const tenant = params.tenant ?? ""
  const agent = params.agent ?? ""
  const model = params.model ?? ""

  const data = await loadCost(range)

  if (!data) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Cost"
          description="Spend by model, agent, tenant, day."
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

  // Apply client-style filtering server-side. Since the runtime's
  // `cost` endpoint does not yet accept tenant/agent/model filters,
  // we filter the returned breakdowns locally so the UI is responsive
  // and consistent. When the runtime adds those filters, swap to
  // passing them through `api.cost(...)` and drop the local filter.
  const filtered: CostSummary = {
    ...data,
    by_tenant: tenant
      ? data.by_tenant.filter((t) => t.tenant_id === tenant)
      : data.by_tenant,
    by_agent: agent
      ? data.by_agent.filter((a) => a.agent === agent)
      : data.by_agent,
    by_model: model
      ? data.by_model.filter((m) => m.model === model)
      : data.by_model,
  }

  const totalIsZero =
    filtered.period_total_usd === 0 && filtered.by_day.length === 0

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Cost"
        description="Spend by model, agent, tenant, day. Budget alerts and forecast."
      />

      <CostFilters
        data={filtered}
        initialRange={range as "1d" | "7d" | "30d" | "90d"}
        initialTenant={tenant}
        initialAgent={agent}
        initialModel={model}
      />

      {totalIsZero ? (
        <Empty className="border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <CircleDollarSign />
            </EmptyMedia>
            <EmptyTitle>No spend recorded</EmptyTitle>
            <EmptyDescription>
              Cost tracking starts the moment an agent invokes a billed model.
              Run an agent or extend the time range to see data here.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <>
          <CostHeader data={filtered} />
          <CostAreaChart data={filtered} />
          <CostBreakdowns data={filtered} />
        </>
      )}
    </div>
  )
}
