// SPDX-License-Identifier: Apache-2.0

import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { CustomerSidebar } from "@/components/layout/customer-sidebar"
import { CustomerTopbar } from "@/components/layout/customer-topbar"
import { requireCustomerContext } from "@/lib/session"

// All /(app)/* routes are session-dependent and runtime-data-backed.
export const dynamic = "force-dynamic"

export default async function AppLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const { session, ctx } = await requireCustomerContext()

  return (
    <SidebarProvider>
      <CustomerSidebar />
      <SidebarInset>
        <CustomerTopbar
          user={{ name: session.user.name, email: session.user.email }}
          tenantName={ctx.tenantName}
        />
        <div className="flex-1 p-6">{children}</div>
      </SidebarInset>
    </SidebarProvider>
  )
}
