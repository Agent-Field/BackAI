// Home dashboard — the first thing operators see when they sign in.
//
// Server-renders a snapshot from `api.home()` and falls back to a
// "runtime unreachable" empty state on error. Every panel below is
// designed to render cleanly with zero data: empty arrays produce
// flat sparklines, empty tables produce inline empty states, and the
// alerts panel hides itself entirely when there is nothing to show.

import {
  Activity,
  AlertTriangle,
  Boxes,
  CircleDollarSign,
  ServerCrash,
} from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { api, type HomeOverview } from "@/lib/api"

import { AlertsList } from "./_home/alerts-list"
import { KpiCard } from "./_home/kpi-card"
import { RunsTable } from "./_home/runs-table"
import { WebhookList } from "./_home/webhook-list"

export const dynamic = "force-dynamic"

function formatCurrency(usd: number): string {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: usd >= 1 ? 2 : 4,
    maximumFractionDigits: 4,
  }).format(usd)
}

function formatPercent(rate: number): string {
  // `rate` arrives as a 0..1 fraction (e.g. 0.024 → "2.4%").
  return `${(rate * 100).toFixed(1)}%`
}

function formatCompact(n: number): string {
  return new Intl.NumberFormat("en-US", {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(n)
}

async function loadHome(): Promise<HomeOverview | null> {
  try {
    return await api.home()
  } catch {
    return null
  }
}

export default async function HomePage() {
  const data = await loadHome()

  if (!data) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Home"
          description="What's happening right now across your stack."
        />
        <Empty className="border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ServerCrash />
            </EmptyMedia>
            <EmptyTitle>Runtime unreachable</EmptyTitle>
            <EmptyDescription>
              The dashboard could not reach the agent runtime. Activity, runs,
              and cost data will appear here once the runtime is online.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Home"
        description="What's happening right now across your stack."
      />

      <AlertsList alerts={data.alerts} />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard
          label="Requests / min"
          value={formatCompact(data.requests_per_minute)}
          description="Trailing 60 minutes."
          icon={Activity}
          sparkline={data.request_sparkline}
        />
        <KpiCard
          label="Error rate"
          value={formatPercent(data.error_rate)}
          description="Trailing 60 minutes."
          icon={AlertTriangle}
          sparkline={data.error_sparkline}
        />
        <KpiCard
          label="Cost today"
          value={formatCurrency(data.cost_today_usd)}
          description="Resets at 00:00 UTC."
          icon={CircleDollarSign}
          sparkline={data.cost_sparkline}
        />
        <KpiCard
          label="Queue depth"
          value={formatCompact(data.queue_depth)}
          description="Jobs waiting to run."
          icon={Boxes}
          sparkline={data.queue_sparkline}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <div className="xl:col-span-2">
          <RunsTable runs={data.recent_runs} />
        </div>
        <WebhookList deliveries={data.recent_webhook_deliveries} />
      </div>
    </div>
  )
}
