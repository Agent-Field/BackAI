// SPDX-License-Identifier: Apache-2.0

import { redirect } from "next/navigation"

import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { AppSidebar } from "@/components/layout/app-sidebar"
import { Topbar } from "@/components/layout/topbar"
import { operatorCount, requireOperator } from "@/lib/session"

// Admin routes are session-dependent and runtime-data-backed. Never
// prerender — every request needs a fresh session check + live data.
export const dynamic = "force-dynamic"

function billingDisabled(): boolean {
  return process.env.AF_STACK_BILLING_ADAPTER?.trim().toLowerCase() === "none"
}

function showShipwright(): boolean {
  return process.env.AF_STACK_SHOW_SHIPWRIGHT?.trim().toLowerCase() === "true"
}

export default async function AdminLayout({ children }: { children: React.ReactNode }) {
  // First-run: if no operator exists, divert to setup wizard.
  const count = await operatorCount()
  if (count === 0) {
    redirect("/setup")
  }

  const session = await requireOperator()
  const isBillingDisabled = billingDisabled()
  const isShipwrightVisible = showShipwright()

  return (
    <SidebarProvider>
      <AppSidebar billingDisabled={isBillingDisabled} showShipwright={isShipwrightVisible} />
      <SidebarInset>
        <Topbar
          billingDisabled={isBillingDisabled}
          showShipwright={isShipwrightVisible}
          user={{
            name: session.user.name,
            email: session.user.email,
          }}
        />
        {/* Edit via plugins: fork-specific operator views should mount under apps/dashboard/plugins. */}
        <div className="min-w-0 flex-1 overflow-x-hidden p-6">{children}</div>
      </SidebarInset>
    </SidebarProvider>
  )
}
