// Billing config — points operator at the customer-facing billing view
// and shows the adapter status (Stripe live vs stub).

import { ArrowRight, CreditCard, Receipt, Users } from "lucide-react"
import Link from "next/link"

import { PageHeader } from "@/components/layout/page-header"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { api } from "@/lib/api"

export const dynamic = "force-dynamic"

export default async function Page() {
  const stripeKey = process.env.STRIPE_SECRET_KEY
  const stubMode = !stripeKey
  let customerCount = 0
  let meterCount = 0
  try {
    const customers = await api.billing.customers()
    customerCount = customers.customers.length
    const meters = await api.billing.meters()
    meterCount = meters.meters.length
  } catch {
    /* ok — KPIs show 0 */
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Billing"
        description="Stripe + meter configuration that powers the customer-facing billing view."
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <CreditCard className="size-3" />
              Stripe adapter
            </CardDescription>
            <CardTitle className="text-2xl font-semibold tracking-tight">
              {stubMode ? "Stub mode" : "Live"}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-muted-foreground text-xs">
              {stubMode
                ? "Set STRIPE_SECRET_KEY to switch to live Stripe."
                : "Connected to live Stripe — usage syncs on Stripe webhook events."}
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <Users className="size-3" />
              Customers
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {customerCount}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-muted-foreground text-xs">
              Tenants with a billing record provisioned.
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <Receipt className="size-3" />
              Active meters
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {meterCount}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-muted-foreground text-xs">
              Usage rows in the current period.
            </p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Customer-facing billing</CardTitle>
          <CardDescription>
            The page your end users see for plan + usage. Drilldown view
            with per-tenant Stripe Portal link.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button
            render={
              <Link href="/customers/customer-billing">
                Open customer billing
                <ArrowRight data-icon="inline-end" />
              </Link>
            }
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Supported meters</CardTitle>
          <CardDescription>
            Meters the runtime pushes into{" "}
            <code className="font-mono">suite_usage_meters</code> for
            billing aggregation.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap gap-2">
            <Badge variant="secondary">llm_tokens</Badge>
            <Badge variant="secondary">sandbox_seconds</Badge>
            <Badge variant="secondary">webhook_deliveries</Badge>
            <Badge variant="secondary">notifications_sent</Badge>
            <Badge variant="outline">workload-module custom</Badge>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Stripe webhook</CardTitle>
          <CardDescription>
            Point your Stripe webhook at{" "}
            <code className="font-mono">POST /webhooks/in/stripe</code>{" "}
            for subscription + invoice updates.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <pre className="bg-muted overflow-x-auto rounded-md p-3 text-xs">
            stripe listen --forward-to localhost:38080/webhooks/in/stripe
          </pre>
        </CardContent>
      </Card>
    </div>
  )
}
