// SPDX-License-Identifier: Apache-2.0

import { AppSidebar } from "@/components/app-sidebar"
import { TopBar } from "@/components/layout/top-bar"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"

import { api } from "@/lib/api"
import { requireOperator } from "@/lib/session"
import { layout } from "@/lib/theme"

// Shared shell for every authenticated dashboard route. Lives at the
// (dashboard) route-group level so the sidebar + top bar persist across
// navigation without re-rendering. Per-page layout details (breadcrumb,
// page-specific actions) belong in the page, not here.

export default async function DashboardLayout({
  children,
}: {
  children: React.ReactNode
}) {
  // Real session validation — middleware only checks the cookie EXISTS,
  // not that it matches a row in the session table. A stale cookie left
  // over from a previous dashboard run gets past middleware but every
  // server-side admin call fails with OPERATOR_AUTH_REQUIRED. requireOperator
  // queries the DB and redirects to /login when the session is gone or
  // the user no longer has an operator role.
  await requireOperator()

  // Top bar needs the initial anchors snapshot + the tenant list so the
  // first paint shows real values. Failures degrade silently — the
  // top bar polls every 5s and recovers.
  const [anchors, tenants] = await Promise.allSettled([
    api.admin.anchors.get(),
    api.admin.tenants.list(),
  ])

  const initialAnchors = anchors.status === "fulfilled" ? anchors.value : null
  const tenantList =
    tenants.status === "fulfilled"
      ? tenants.value.tenants.map((t) => ({ id: t.id, name: t.name }))
      : []

  return (
    <SidebarProvider
      style={{ ["--sidebar-width" as never]: `${layout.sidebarWidth}px` }}
    >
      <AppSidebar />
      <SidebarInset>
        <TopBar
          initialAnchors={initialAnchors}
          tenants={tenantList}
        />
        {children}
      </SidebarInset>
    </SidebarProvider>
  )
}
