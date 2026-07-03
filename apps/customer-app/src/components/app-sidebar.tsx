"use client"

import * as React from "react"

import { NavMain } from "@/components/nav-main"
import { NavUser } from "@/components/nav-user"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"
import { brand } from "@/lib/brand"
import { LayoutDashboardIcon, SparklesIcon } from "lucide-react"
import Link from "next/link"

interface AppSidebarProps extends React.ComponentProps<typeof Sidebar> {
  user?: { name?: string | null; email?: string | null }
}

// Honest customer-app sidebar: the brand, the surfaces that actually exist
// (just Dashboard for now), and the real signed-in user. The shadcn starter's
// fake nav (Acme Inc, Playground/Models, Design Engineering/Sales/Travel, a
// hardcoded "shadcn" user) is gone — a fork adds nav here as it adds real
// pages; nav-projects/nav-secondary remain available as building blocks.
export function AppSidebar({ user, ...props }: AppSidebarProps) {
  return (
    <Sidebar variant="inset" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" render={<Link href="/dashboard" />}>
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                <SparklesIcon className="size-4" aria-hidden />
              </div>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-medium">{brand.displayName}</span>
                <span className="truncate text-xs">Customer</span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <NavMain
          items={[
            {
              title: "Dashboard",
              url: "/dashboard",
              icon: <LayoutDashboardIcon />,
              isActive: true,
            },
          ]}
        />
      </SidebarContent>
      <SidebarFooter>
        <NavUser
          user={{
            name: user?.name ?? "",
            email: user?.email ?? "",
            avatar: "",
          }}
        />
      </SidebarFooter>
    </Sidebar>
  )
}
