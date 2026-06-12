// SPDX-License-Identifier: Apache-2.0

import {
  ActivityIcon,
  CoinsIcon,
  CreditCardIcon,
  WalletIcon,
} from "lucide-react"

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, type CostEvent } from "@/lib/api"
import { requireCustomerContext } from "@/lib/session"
import { ApiKeyPanel } from "./api-key-panel"
import { QuickstartPanel } from "./quickstart-panel"

function formatUSD(n: number): string {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: n < 1 ? 4 : 2,
    maximumFractionDigits: n < 1 ? 6 : 2,
  }).format(n)
}

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

type Loaded = {
  costToday: number
  callsToday: number
  recentCalls: CostEvent[]
  balanceRemaining: number | null
  plan: string
}

async function load(tenantId: string): Promise<Loaded> {
  const events: CostEvent[] = []
  let costToday = 0
  let callsToday = 0
  let balanceRemaining: number | null = null
  let plan = "free"

  try {
    const today = new Date()
    today.setHours(0, 0, 0, 0)
    const summary = await api.cost({
      tenant: tenantId,
      from: today.toISOString(),
    })
    costToday = summary.period_total_usd
  } catch {
    costToday = 0
  }

  try {
    const recent = await api.costEvents({ tenant: tenantId, limit: 10 })
    events.push(...recent.events)
    const cutoff = new Date()
    cutoff.setHours(0, 0, 0, 0)
    callsToday = recent.events.filter(
      (e) => new Date(e.occurred_at).getTime() >= cutoff.getTime(),
    ).length
  } catch {
    // no events yet — fine.
  }

  try {
    const customer = await api.billing.customer(tenantId)
    plan = customer.plan
  } catch {
    // billing customer may not exist yet
  }

  return { costToday, callsToday, recentCalls: events, balanceRemaining, plan }
}

export default async function DashboardPage() {
  const { session, ctx } = await requireCustomerContext()
  const data = await load(ctx.tenantId)

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">
          Support workspace
        </h1>
        <p className="text-muted-foreground text-sm">
          {ctx.tenantName} · {session.user.email}
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-2">
              <ActivityIcon className="size-4" />
              AI actions today
            </CardDescription>
            <CardTitle className="text-3xl font-semibold tabular-nums">
              {data.callsToday}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-2">
              <CoinsIcon className="size-4" />
              Cost today
            </CardDescription>
            <CardTitle className="text-3xl font-semibold tabular-nums">
              {formatUSD(data.costToday)}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-2">
              <WalletIcon className="size-4" />
              Balance remaining
            </CardDescription>
            <CardTitle className="text-3xl font-semibold tabular-nums">
              {data.balanceRemaining === null
                ? "No budget"
                : formatUSD(data.balanceRemaining)}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-2">
              <CreditCardIcon className="size-4" />
              Plan
            </CardDescription>
            <CardTitle className="text-3xl font-semibold capitalize">
              {data.plan}
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <ApiKeyPanel prefix={ctx.apiKeyPrefix} />
        <QuickstartPanel prefix={ctx.apiKeyPrefix} />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Recent calls</CardTitle>
          <CardDescription>
            Last 10 model calls billed to your tenant.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {data.recentCalls.length === 0 ? (
            <p className="text-muted-foreground py-6 text-center text-sm">
              No calls yet. Open Support Desk to draft a reply and see one land here.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Model</TableHead>
                  <TableHead className="text-right">Prompt</TableHead>
                  <TableHead className="text-right">Completion</TableHead>
                  <TableHead className="text-right">Cost</TableHead>
                  <TableHead>When</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.recentCalls.map((e) => (
                  <TableRow key={e.id}>
                    <TableCell className="font-mono text-xs">
                      {e.model}
                      {e.cached ? (
                        <Badge variant="outline" className="ml-2">
                          cached
                        </Badge>
                      ) : null}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {e.prompt_tokens}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {e.completion_tokens}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatUSD(e.cost_usd)}
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs">
                      {formatTime(e.occurred_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
