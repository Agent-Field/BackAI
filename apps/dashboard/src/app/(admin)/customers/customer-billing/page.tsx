// SPDX-License-Identifier: Apache-2.0

// Customer Billing page — per-tenant usage and cost rollup.

import { ReceiptText } from "lucide-react"
import Link from "next/link"

import { PageHeader } from "@/components/layout/page-header"
import { MultiTenancyRequired } from "@/components/layout/tab-stub"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"

import { CustomerBillingView } from "./_components/billing-view"

export const dynamic = "force-dynamic"

function billingDisabled(): boolean {
  return process.env.AF_STACK_BILLING_ADAPTER?.trim().toLowerCase() === "none"
}

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
  if (billingDisabled()) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Customer Billing"
          description="Billing is disabled for this deployment."
        />
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ReceiptText />
            </EmptyMedia>
            <EmptyTitle>Billing adapter disabled</EmptyTitle>
            <EmptyDescription>
              Set <code className="font-mono">AF_STACK_BILLING_ADAPTER</code> to{" "}
              <code className="font-mono">stripe</code> or another supported adapter, configure the
              provider env vars, and restart the stack.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="outline"
              render={
                <Link
                  href="https://github.com/Agent-Field/af-stack/blob/main/docs/adapters/billing.md"
                  target="_blank"
                >
                  Enable billing
                </Link>
              }
            />
          </EmptyContent>
        </Empty>
      </div>
    )
  }
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Customer Billing"
        description="Stripe customers + per-tenant usage meters for the current billing period."
      />
      <CustomerBillingView />
    </div>
  )
}
