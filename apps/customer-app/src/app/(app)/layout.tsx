// SPDX-License-Identifier: Apache-2.0

import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { CustomerSidebar } from "@/components/layout/customer-sidebar"
import { CustomerTopbar } from "@/components/layout/customer-topbar"
import { requireCustomerContext } from "@/lib/session"

// All /(app)/* routes are session-dependent and runtime-data-backed.
export const dynamic = "force-dynamic"

function billingDisabled(): boolean {
  return process.env.AF_STACK_BILLING_ADAPTER?.trim().toLowerCase() === "none"
}

export default async function AppLayout({ children }: { children: React.ReactNode }) {
  const { session, ctx } = await requireCustomerContext()

  return (
    <SidebarProvider className="h-svh min-h-svh overflow-hidden">
      <CustomerSidebar billingDisabled={billingDisabled()} />
      <SidebarInset className="min-h-0 min-w-0 overflow-hidden">
        <CustomerTopbar
          user={{ name: session.user.name, email: session.user.email }}
          tenantName={ctx.tenantName}
        />
        {/* Edit freely: logged-in product pages render inside this shell. */}
        <div className="min-h-0 flex-1 overflow-auto p-6">{children}</div>
      </SidebarInset>
    </SidebarProvider>
  )
}
