// SPDX-License-Identifier: Apache-2.0

import { AppSidebar } from "@/components/app-sidebar"
import { HomeShell } from "@/components/home/home-shell"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbList,
  BreadcrumbPage,
} from "@/components/ui/breadcrumb"
import { Separator } from "@/components/ui/separator"
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar"

import { getHomeSnapshot } from "@/lib/home/data"

// Home page (route "/").
//
// Server-renders the initial HomeSnapshot then hands it to the Home shell
// for presentation. The page is fully dynamic — middleware-gated routes
// can't be statically cached anyway. WebSocket upgrade for live ticks
// is tracked separately (home.md §11) and not blocking v1; for now the
// browser refreshes on navigation.

export const dynamic = "force-dynamic"

export default async function HomePage() {
  const snapshot = await getHomeSnapshot()
  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <header className="flex h-16 shrink-0 items-center gap-inline px-page-x">
          <SidebarTrigger className="-ml-1" />
          <Separator
            orientation="vertical"
            className="mr-2 data-vertical:h-4 data-vertical:self-auto"
          />
          <Breadcrumb>
            <BreadcrumbList>
              <BreadcrumbItem>
                <BreadcrumbPage>Home</BreadcrumbPage>
              </BreadcrumbItem>
            </BreadcrumbList>
          </Breadcrumb>
        </header>
        <main className="flex flex-1 flex-col gap-section px-page-x py-page-y">
          <HomeShell snapshot={snapshot} />
        </main>
      </SidebarInset>
    </SidebarProvider>
  )
}
