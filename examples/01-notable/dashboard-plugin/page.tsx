// SPDX-License-Identifier: Apache-2.0

// Notable dashboard plugin page (example 01).
//
// Shows per-tenant note counts + the matching LLM token spend so an
// operator can answer "which tenant is leaning on the AI features
// hardest?" at a glance.
//
// Data source: the Notable API service exposes GET /notable/stats. We
// reach it via NOTABLE_API_URL (set in docker-compose.yml) so server-
// side fetches stay inside the docker network and no client secret
// ever touches the browser.
//
// Degrades gracefully: when the Notable API is unreachable (example
// not running, or the operator removed the service) the page renders a
// calm empty state instead of crashing.

import { Notebook, ServerCrash } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import {
  Card,
  CardContent,
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

export const dynamic = "force-dynamic"

type TenantRow = {
  tenant_id: string
  note_count: number
  cost_usd: number
}

type NotableStats = {
  tenants: TenantRow[]
  generated_at: string
}

const fmtUSD = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 4,
})

async function loadStats(): Promise<NotableStats | null> {
  // Server-side fetch uses the in-network URL. The fallback covers the
  // case where someone serves the dashboard outside docker-compose.
  const base =
    process.env.NOTABLE_API_URL?.trim() || "http://notable-api:8090"
  try {
    const res = await fetch(`${base}/notable/stats`, {
      cache: "no-store",
      // Match the API service's expected header. The stats endpoint
      // bypasses RLS internally, so the tenant id is informational
      // here — we send "system" so the audit trail attributes it.
      headers: { "x-af-stack-tenant-id": "system" },
    })
    if (!res.ok) return null
    return (await res.json()) as NotableStats
  } catch {
    return null
  }
}

export default async function NotableDashboardPage() {
  const stats = await loadStats()

  if (!stats) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Notable"
          description="Per-tenant note count + token spend from the Notable example."
        />
        <Empty className="border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ServerCrash />
            </EmptyMedia>
            <EmptyTitle>Notable API unreachable</EmptyTitle>
            <EmptyDescription>
              Could not reach <code>NOTABLE_API_URL</code>. Spin up the
              Notable example with
              <code> docker compose -f examples/01-notable/docker-compose.yml up </code>
              and refresh.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  const totalNotes = stats.tenants.reduce(
    (acc, t) => acc + t.note_count,
    0,
  )
  const totalSpend = stats.tenants.reduce((acc, t) => acc + t.cost_usd, 0)

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Notable"
        description="Per-tenant note count + token spend from the Notable example."
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardDescription>Notes across all tenants</CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {totalNotes.toLocaleString()}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription>LLM spend (current period)</CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {fmtUSD.format(totalSpend)}
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <Card className="gap-0">
        <CardHeader className="border-b pb-4">
          <CardTitle>Tenants</CardTitle>
          <CardDescription>
            Click a tenant id to drill into its multi-tenancy detail page.
          </CardDescription>
        </CardHeader>
        {stats.tenants.length === 0 ? (
          <Empty className="border-0">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Notebook />
              </EmptyMedia>
              <EmptyTitle>No notes yet</EmptyTitle>
              <EmptyDescription>
                Run <code>examples/01-notable/scripts/seed.sh</code> to
                seed two tenants + a handful of notes per tenant.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <CardContent className="p-0">
            <ul className="divide-y">
              {stats.tenants.map((t) => (
                <li
                  key={t.tenant_id}
                  className="grid grid-cols-[1fr_auto_auto] items-center gap-4 px-4 py-3 text-sm"
                >
                  <a
                    href={`/customers/tenants/${encodeURIComponent(t.tenant_id)}`}
                    className="truncate font-medium hover:underline"
                    title={t.tenant_id}
                  >
                    {t.tenant_id}
                  </a>
                  <div className="text-muted-foreground w-24 text-right tabular-nums">
                    {t.note_count.toLocaleString()} notes
                  </div>
                  <div className="w-24 text-right font-mono text-sm tabular-nums">
                    {fmtUSD.format(t.cost_usd)}
                  </div>
                </li>
              ))}
            </ul>
          </CardContent>
        )}
      </Card>
    </div>
  )
}