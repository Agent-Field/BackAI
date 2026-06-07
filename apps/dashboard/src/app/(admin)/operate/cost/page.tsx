// SPDX-License-Identifier: Apache-2.0

// Cost dashboard — the hero tab. Server-renders snapshots from
// `api.cost()`, `api.llm.cacheStats()`, `api.budgets.list()`, and the
// first page of `api.costEvents()` in parallel, then hands them to
// client components for interactivity (filters, live event stream,
// budget dialog).
//
// Everything is degrade-gracefully: each fetch is wrapped in
// try/catch and falls back to null. The page is designed to render
// cleanly with zero data — no scary errors when the runtime is fresh.

import { CircleDollarSign, ServerCrash } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  api,
  type Budget,
  type CacheStats,
  type CostEvent,
  type CostSummary,
  type Tenant,
} from "@/lib/api"

import { CostAreaChart } from "./_components/cost-area-chart"
import { CostBreakdowns } from "./_components/cost-breakdowns"
import { CostBudgetAlerts } from "./_components/cost-budget-alerts"
import { CostBudgetDialog } from "./_components/cost-budget-dialog"
import { CostEventStream } from "./_components/cost-event-stream"
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

// Each fetch is independent; resolve them in parallel and ignore
// individual failures so a single broken endpoint never blanks the
// page. Returns null for whichever fetches couldn't complete.
type Snapshot = {
  cost: CostSummary | null
  cache: CacheStats | null
  budgets: Budget[]
  tenants: Tenant[]
  events: CostEvent[]
  multiTenancyEnabled: boolean
}

async function loadSnapshot(range: string): Promise<Snapshot> {
  const days = rangeToDays(range)
  const from = isoFromDaysAgo(days)
  const to = new Date().toISOString()

  const [
    costResult,
    cacheResult,
    budgetsResult,
    tenantsResult,
    eventsResult,
    modulesResult,
  ] = await Promise.allSettled([
    api.cost({ from, to }),
    api.llm.cacheStats(),
    api.budgets.list(),
    api.admin.tenants.list(),
    api.costEvents({ limit: 50, from, to }),
    api.modulesState(),
  ])

  return {
    cost: costResult.status === "fulfilled" ? costResult.value : null,
    cache: cacheResult.status === "fulfilled" ? cacheResult.value : null,
    budgets:
      budgetsResult.status === "fulfilled"
        ? budgetsResult.value.budgets
        : [],
    tenants:
      tenantsResult.status === "fulfilled"
        ? tenantsResult.value.tenants
        : [],
    events:
      eventsResult.status === "fulfilled" ? eventsResult.value.events : [],
    multiTenancyEnabled:
      modulesResult.status === "fulfilled"
        ? modulesResult.value.multi_tenancy_enabled
        : false,
  }
}

/**
 * Pick the budget that's most relevant to display in the hero strip.
 * When the active tenant filter matches a budget, use it. Otherwise
 * fall back to the account-wide `budget_usd` returned by the cost
 * summary (which may be null when nothing is configured).
 */
function pickHeroBudget(
  budgets: Budget[],
  costSummary: CostSummary,
  tenantFilter: string,
): number | null {
  if (tenantFilter) {
    const b = budgets.find((x) => x.tenant_id === tenantFilter)
    if (b) return b.monthly_usd
  }
  if (budgets.length === 1) {
    return budgets[0].monthly_usd
  }
  if (budgets.length > 1) {
    return budgets.reduce((acc, b) => acc + b.monthly_usd, 0)
  }
  return costSummary.budget_usd
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

  const snapshot = await loadSnapshot(range)
  const { cost, cache, budgets, tenants, events, multiTenancyEnabled } =
    snapshot

  const headerActions = (
    <CostBudgetDialog
      tenants={tenants}
      multiTenancyEnabled={multiTenancyEnabled}
      existingBudgets={budgets}
    />
  )

  if (!cost) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Cost"
          description="Spend by model, agent, tenant, day."
          actions={headerActions}
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

  // Filter the breakdowns locally — the cost endpoint does not yet
  // accept tenant/agent/model filters. When it does, push these through
  // `api.cost(...)` and drop the local filter step.
  const filtered: CostSummary = {
    ...cost,
    by_tenant: tenant
      ? cost.by_tenant.filter((t) => t.tenant_id === tenant)
      : cost.by_tenant,
    by_agent: agent
      ? cost.by_agent.filter((a) => a.agent === agent)
      : cost.by_agent,
    by_model: model
      ? cost.by_model.filter((m) => m.model === model)
      : cost.by_model,
  }

  const heroBudget = pickHeroBudget(budgets, filtered, tenant)
  const totalIsZero =
    filtered.period_total_usd === 0 && filtered.by_day.length === 0

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Cost"
        description="Spend by model, agent, tenant, day. Budget alerts, forecast, and a live event stream."
        actions={headerActions}
      />

      <CostFilters
        data={filtered}
        events={events}
        initialRange={range as "1d" | "7d" | "30d" | "90d"}
        initialTenant={tenant}
        initialAgent={agent}
        initialModel={model}
      />

      <CostBudgetAlerts budgets={budgets} tenants={tenants} />

      <CostHeader data={filtered} cache={cache} budgetUsd={heroBudget} />

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
          <CostAreaChart data={filtered} />
          <CostBreakdowns data={filtered} />
        </>
      )}

      <CostEventStream
        initialEvents={events}
        tenant={tenant}
        model={model}
      />
    </div>
  )
}