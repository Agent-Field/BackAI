// SPDX-License-Identifier: Apache-2.0

"use client"

import { LayoutDashboard, LogOut, Sparkles } from "lucide-react"
import Link from "next/link"
import * as React from "react"

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"
import { brand } from "@/lib/brand"

interface AppSidebarProps extends React.ComponentProps<typeof Sidebar> {
  user?: { name?: string | null; email?: string | null }
}

// Honest, minimal customer-app sidebar: the brand, the one real surface
// (Dashboard), and the signed-in user with sign-out. (Replaced the shadcn
// starter template's fake Acme Inc / Playground / Models / Projects nav.)
export function AppSidebar({ user, ...props }: AppSidebarProps) {
  return (
    <Sidebar variant="inset" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" render={<Link href="/dashboard" />}>
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                <Sparkles className="size-4" aria-hidden />
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
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton render={<Link href="/dashboard" />}>
                  <LayoutDashboard aria-hidden />
                  <span>Dashboard</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <SidebarMenu>
          {user?.email ? (
            <SidebarMenuItem>
              <div className="flex min-w-0 flex-col px-2 py-1 leading-tight">
                {user.name ? (
                  <span className="truncate text-sm font-medium">{user.name}</span>
                ) : null}
                <span className="truncate text-xs text-muted-foreground">{user.email}</span>
              </div>
            </SidebarMenuItem>
          ) : null}
          <SidebarMenuItem>
            <SidebarMenuButton render={<Link href="/sign-out" />}>
              <LogOut aria-hidden />
              <span>Sign out</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
    </Sidebar>
  )
}
