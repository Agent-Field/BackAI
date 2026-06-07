// SPDX-License-Identifier: Apache-2.0

import { CalendarIcon, CreditCardIcon, GaugeIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { api, type BillingCustomer, type UsageMeterList } from "@/lib/api"
import { requireCustomerContext } from "@/lib/session"
import { ManageSubscriptionButton } from "./manage-subscription-button"

function formatUSD(n: number): string {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: n < 1 ? 4 : 2,
    maximumFractionDigits: n < 1 ? 6 : 2,
  }).format(n)
}

function formatDate(iso: string | null): string {
  if (!iso) return "—"
  try {
    return new Date(iso).toLocaleDateString()
  } catch {
    return iso
  }
}

export const dynamic = "force-dynamic"

export default async function BillingPage() {
  const { ctx } = await requireCustomerContext()

  let customer: BillingCustomer | null = null
  let meters: UsageMeterList | null = null

  try {
    customer = await api.billing.customer(ctx.tenantId)
  } catch {
    // not provisioned yet — show stub
  }
  try {
    meters = await api.billing.meters({ tenant: ctx.tenantId })
  } catch {
    meters = null
  }

  const plan = customer?.plan ?? "free"
  const periodEnd = customer?.current_period_end ?? null
  const status = customer?.subscription_status ?? "stub"
  const totalThisPeriod = meters?.total_cost_usd ?? 0

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Billing</h1>
        <p className="text-muted-foreground text-sm">
          Plan, usage, and the customer portal for {ctx.tenantName}.
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-2">
              <CreditCardIcon className="size-4" />
              Plan
            </CardDescription>
            <CardTitle className="text-2xl font-semibold capitalize">
              {plan}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <Badge variant="outline" className="font-mono text-[10px]">
              {status}
            </Badge>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-2">
              <CalendarIcon className="size-4" />
              Period ends
            </CardDescription>
            <CardTitle className="text-2xl font-semibold tabular-nums">
              {formatDate(periodEnd)}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-2">
              <GaugeIcon className="size-4" />
              Total this period
            </CardDescription>
            <CardTitle className="text-2xl font-semibold tabular-nums">
              {formatUSD(totalThisPeriod)}
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Usage meters</CardTitle>
          <CardDescription>
            Live counts for the current billing period.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {!meters || meters.meters.length === 0 ? (
            <p className="text-muted-foreground py-6 text-center text-sm">
              No metered usage yet.
            </p>
          ) : (
            <div className="flex flex-col gap-4">
              {meters.meters.map((m) => {
                const ratio =
                  totalThisPeriod > 0 && m.cost_usd !== null
                    ? (m.cost_usd / totalThisPeriod) * 100
                    : 0
                return (
                  <div key={m.meter} className="flex flex-col gap-1">
                    <div className="flex items-center justify-between text-sm">
                      <span className="font-medium">{m.meter}</span>
                      <span className="tabular-nums text-muted-foreground">
                        {m.quantity.toLocaleString()} units
                        {m.cost_usd !== null
                          ? ` · ${formatUSD(m.cost_usd)}`
                          : ""}
                      </span>
                    </div>
                    <Progress value={ratio} />
                  </div>
                )
              })}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Subscription</CardTitle>
          <CardDescription>
            Open the Stripe customer portal to change plans, update payment, or
            view invoices.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ManageSubscriptionButton tenantId={ctx.tenantId} />
        </CardContent>
      </Card>
    </div>
  )
}
