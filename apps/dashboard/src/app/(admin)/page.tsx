// SPDX-License-Identifier: Apache-2.0

// Home dashboard — the first thing operators see when they sign in.
//
// Server-renders a snapshot from `api.home()` and falls back to a
// "runtime unreachable" empty state on error. Every panel below is
// designed to render cleanly with zero data: empty arrays produce
// flat sparklines, empty tables produce inline empty states, and the
// alerts panel hides itself entirely when there is nothing to show.

import { Activity, AlertTriangle, Boxes, CircleDollarSign, ServerCrash } from "lucide-react"
import Link from "next/link"

import { PageHeader } from "@/components/layout/page-header"
import { GuidedTour } from "@/components/guided-tour"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { api, type HomeOverview } from "@/lib/api"

import { AlertsList } from "./_home/alerts-list"
import { GettingStartedPanel, type GettingStartedState } from "./_home/getting-started-panel"
import { KpiCard } from "./_home/kpi-card"
import { RunsTable } from "./_home/runs-table"
import { WebhookList } from "./_home/webhook-list"

export const dynamic = "force-dynamic"

function formatCurrency(usd: number): string {
  const abs = Math.abs(usd)
  const digits = abs === 0 ? 2 : abs < 0.0001 ? 6 : abs < 0.01 ? 4 : 2
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
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

function runtimeCatalogUrl(): string {
  const base = process.env.NEXT_PUBLIC_RUNTIME_UI_URL ?? "http://localhost:8081"
  return `${base.replace(/\/$/, "")}/api/v1/discovery/capabilities`
}

function liteLLMUrl(): string {
  return process.env.NEXT_PUBLIC_LITELLM_UI_URL ?? "http://localhost:4000/ui"
}

function StackProofPanel() {
  const services = [
    { label: "Postgres", detail: "tenant data + usage ledger", href: "/build/database" },
    { label: "better-auth", detail: "sessions + operator login", href: "/build/auth" },
    {
      label: "LLM gateway",
      detail: "provider routing + token cost",
      href: liteLLMUrl(),
      external: true,
    },
    {
      label: "agent runtime",
      detail: "workflow catalog + traces",
      href: runtimeCatalogUrl(),
      external: true,
    },
    { label: "billing", detail: "plans, budgets, cost events", href: "/operate/cost" },
    { label: "webhooks", detail: "delivery plumbing", href: "/build/webhooks" },
  ]

  return (
    <Card data-tour="admin-stack-services">
      <CardHeader className="pb-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle className="text-base">Backend stack in this demo</CardTitle>
            <CardDescription>
              The first SupportDesk action is backed by these bundled services.
            </CardDescription>
          </div>
          <Button
            variant="outline"
            size="sm"
            nativeButton={false}
            render={<Link href="/build/database">Open database</Link>}
          />
        </div>
      </CardHeader>
      <CardContent>
        <div className="flex flex-wrap gap-2">
          {services.map((service) => (
            <Badge
              key={service.label}
              variant="outline"
              className="gap-1.5 px-2.5 py-1.5"
              render={
                service.external ? (
                  <a href={service.href} target="_blank" rel="noreferrer">
                    <span className="font-medium">{service.label}</span>
                    <span className="text-muted-foreground">{service.detail}</span>
                  </a>
                ) : (
                  <Link href={service.href}>
                    <span className="font-medium">{service.label}</span>
                    <span className="text-muted-foreground">{service.detail}</span>
                  </Link>
                )
              }
            />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

async function loadHome(): Promise<HomeOverview | null> {
  try {
    return await api.home()
  } catch {
    return null
  }
}

async function loadGettingStarted(): Promise<GettingStartedState> {
  const [tenantsResult, keysResult, budgetsResult] = await Promise.allSettled([
    api.admin.tenants.list(),
    api.admin.keys.list(),
    api.budgets.list(),
  ])

  return {
    hasTenant: tenantsResult.status === "fulfilled" && tenantsResult.value.tenants.length > 0,
    hasApiKey:
      keysResult.status === "fulfilled" && keysResult.value.keys.some((key) => !key.revoked_at),
    hasBudget: budgetsResult.status === "fulfilled" && budgetsResult.value.budgets.length > 0,
  }
}

export default async function HomePage() {
  const [data, gettingStarted] = await Promise.all([loadHome(), loadGettingStarted()])

  if (!data) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Home" description="What's happening right now across your stack." />
        <Empty className="border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ServerCrash />
            </EmptyMedia>
            <EmptyTitle>Runtime unreachable</EmptyTitle>
            <EmptyDescription>
              The dashboard could not reach the agent runtime. Activity, runs, and cost data will
              appear here once the runtime is online.
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
        actions={
          <GuidedTour
            id="admin-home-v1"
            autoStart
            steps={[
              {
                element: "[data-tour='admin-getting-started']",
                popover: {
                  title: "Start from the customer action",
                  description:
                    "Open the customer app, draft one reply, then come back here. The admin is meant to prove what the backend did, not be the first thing a user has to understand.",
                  side: "bottom",
                  align: "start",
                },
              },
              {
                element: "[data-tour='admin-stack-services']",
                popover: {
                  title: "Notice what is already wired",
                  description:
                    "The tags expose the important pieces behind the app: Postgres, auth, provider routing, billing, webhooks, and the agent runtime. Services with a UI open directly.",
                  side: "bottom",
                  align: "start",
                },
              },
              {
                element: "[data-tour='admin-kpis']",
                popover: {
                  title: "The backend is already measuring operations",
                  description:
                    "Requests, errors, cost, and queue depth are live runtime signals. After the first SupportDesk reply, cost updates from the gateway ledger.",
                  side: "bottom",
                  align: "start",
                },
              },
              {
                element: "[data-tour='admin-runs']",
                popover: {
                  title: "Requests and workflows land here",
                  description:
                    "BackAI keeps the app-facing record: request, tenant, timing, cost, and links to deeper traces when the workflow has them.",
                  side: "top",
                  align: "start",
                },
              },
              {
                element: "[data-tour='admin-agent-nav']",
                popover: {
                  title: "Follow the evidence trail",
                  description:
                    "Build shows what capabilities are registered. Operate shows runs, cost, queues, logs, and delivery state for the same product action.",
                  side: "right",
                  align: "center",
                },
              },
            ]}
          />
        }
      />

      <div data-tour="admin-getting-started">
        <GettingStartedPanel
          state={gettingStarted}
          customerAppUrl={process.env.CUSTOMER_APP_URL ?? "http://localhost:34000"}
        />
      </div>

      <StackProofPanel />

      <AlertsList alerts={data.alerts} />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4" data-tour="admin-kpis">
        <KpiCard
          label="Requests / min"
          value={formatCompact(data.requests_per_minute)}
          description="Trailing 60 minutes."
          icon={<Activity className="size-4" />}
          sparkline={data.request_sparkline}
        />
        <KpiCard
          label="Error rate"
          value={formatPercent(data.error_rate)}
          description="Trailing 60 minutes."
          icon={<AlertTriangle className="size-4" />}
          sparkline={data.error_sparkline}
        />
        <KpiCard
          label="Cost today"
          value={formatCurrency(data.cost_today_usd)}
          description="Resets at 00:00 UTC."
          icon={<CircleDollarSign className="size-4" />}
          sparkline={data.cost_sparkline}
        />
        <KpiCard
          label="Queue depth"
          value={formatCompact(data.queue_depth)}
          description="Jobs waiting to run."
          icon={<Boxes className="size-4" />}
          sparkline={data.queue_sparkline}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <div className="xl:col-span-2" data-tour="admin-runs">
          <RunsTable runs={data.recent_runs} />
        </div>
        <WebhookList deliveries={data.recent_webhook_deliveries} />
      </div>
    </div>
  )
}
