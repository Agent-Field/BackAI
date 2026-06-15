// SPDX-License-Identifier: Apache-2.0

// Billing page — Build → Billing. The billing integration your customers
// see: plans and subscription state, backed by GET /api/v1/billing/customers.
// Server component: fetched once per render (force-dynamic + no-store via
// lib/api.ts). When the runtime isn't reachable we render a clean empty-state
// shell rather than crashing.

import { CreditCard, Wallet } from "lucide-react"

import { api, ApiError, type BillingCustomerList } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { PageHeader } from "@/components/layout/page-header"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export const dynamic = "force-dynamic"

async function loadCustomers(): Promise<{
  data: BillingCustomerList
  error: string | null
}> {
  try {
    const data = await api.billing.customers()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load billing customers"
    return { data: { customers: [], adapter: "none" }, error: message }
  }
}

export default async function Page() {
  const { data, error } = await loadCustomers()
  const customers = data.customers
  const adapter = data.adapter

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Billing"
        description="The billing integration your customers see — plans and subscription state. Backed by /api/v1/billing/customers."
      />

      {error && customers.length === 0 ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
          <p className="text-foreground mb-1 font-medium">
            Billing isn&apos;t available
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            .
          </p>
        </div>
      ) : (
        <>
          <div className="flex items-center gap-2">
            <Badge variant="secondary" className="text-xs">
              <Wallet className="size-3.5" />
              Adapter: {adapter}
            </Badge>
          </div>

          {adapter === "none" ? (
            <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
              <p className="text-foreground mb-1 font-medium">
                No billing adapter configured
              </p>
              <p>
                This runtime has no billing adapter wired up, so there are no
                customers or subscriptions to show. Configure a provider (e.g.
                Stripe) to enable billing.
              </p>
            </div>
          ) : customers.length === 0 ? (
            <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
              <CreditCard className="size-3.5" />
              No billing customers yet.
            </div>
          ) : (
            <div className="bg-card rounded-xl border">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                      Tenant
                    </TableHead>
                    <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                      Plan
                    </TableHead>
                    <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                      Status
                    </TableHead>
                    <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                      Renews
                    </TableHead>
                    <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                      Email
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {customers.map((customer) => (
                    <TableRow
                      key={customer.tenant_id}
                      className="hover:bg-muted/30"
                    >
                      <TableCell>
                        <code className="font-mono text-xs">
                          {customer.tenant_id}
                        </code>
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className="text-xs">
                          {customer.plan}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-muted-foreground text-xs">
                        {customer.subscription_status ?? "—"}
                      </TableCell>
                      <TableCell className="text-muted-foreground text-xs tabular-nums">
                        {customer.current_period_end ?? "—"}
                      </TableCell>
                      <TableCell className="text-muted-foreground text-xs">
                        {customer.email ?? "—"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </>
      )}

      <p className="text-muted-foreground text-xs">
        Per-tenant invoices and usage live in Customers → Customer Billing.
      </p>
    </div>
  )
}
