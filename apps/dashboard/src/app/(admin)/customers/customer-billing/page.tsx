// Customer Billing page — per-tenant usage and cost rollup.

import { ReceiptText } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import { MultiTenancyRequired } from "@/components/layout/tab-stub"
import { api } from "@/lib/api"

import { CustomerBillingView } from "./_components/billing-view"

export const dynamic = "force-dynamic"

export default async function Page() {
  const modules = await api.modulesState().catch(() => null)
  if (!modules?.multi_tenancy_enabled) {
    return (
      <MultiTenancyRequired
        title="Customer Billing"
        description="Per-tenant billing and invoices"
        icon={ReceiptText}
      />
    )
  }
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Customer Billing"
        description="Per-tenant usage and cost rollup. Real invoicing lands in Phase 10."
      />
      <CustomerBillingView />
    </div>
  )
}
