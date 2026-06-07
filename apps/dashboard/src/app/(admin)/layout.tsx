// SPDX-License-Identifier: Apache-2.0

import { redirect } from "next/navigation"

import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { AppSidebar } from "@/components/layout/app-sidebar"
import { Topbar } from "@/components/layout/topbar"
import { getServerSession, operatorCount } from "@/lib/session"

// Admin routes are session-dependent and runtime-data-backed. Never
// prerender — every request needs a fresh session check + live data.
export const dynamic = "force-dynamic"

export default async function AdminLayout({
  children,
}: {
  children: React.ReactNode
}) {
  // First-run: if no operator exists, divert to setup wizard.
  const count = await operatorCount()
  if (count === 0) {
    redirect("/setup")
  }

  const session = await getServerSession()
  if (!session) {
    // Middleware should have caught this already, but be safe.
    redirect("/login")
  }

  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <Topbar
          user={{
            name: session.user.name,
            email: session.user.email,
          }}
        />
        <div className="min-w-0 flex-1 overflow-x-hidden p-6">{children}</div>
      </SidebarInset>
    </SidebarProvider>
  )
}