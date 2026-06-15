// SPDX-License-Identifier: Apache-2.0

import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { AppSidebar } from "@/components/layout/app-sidebar"
import { Topbar } from "@/components/layout/topbar"
import { requireOperator } from "@/lib/session"

// Admin routes are session-dependent and runtime-data-backed. Never
// prerender — every request needs a fresh session check + live data.
export const dynamic = "force-dynamic"

function billingDisabled(): boolean {
  return process.env.AF_STACK_BILLING_ADAPTER?.trim().toLowerCase() === "none"
}

export default async function AdminLayout({ children }: { children: React.ReactNode }) {
  // A default operator is seeded at boot, so there is no operator-less
  // first-run state to divert to a wizard. requireOperator() sends anyone
  // without a valid operator session to /login.
  const session = await requireOperator()

  return (
    <SidebarProvider>
      <AppSidebar billingDisabled={billingDisabled()} />
      <SidebarInset>
        <Topbar
          billingDisabled={billingDisabled()}
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
