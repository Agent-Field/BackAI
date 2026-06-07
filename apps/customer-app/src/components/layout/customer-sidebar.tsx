// SPDX-License-Identifier: Apache-2.0

"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import {
  Code2,
  CreditCard,
  KeyRound,
  LayoutDashboard,
  LogOut,
  Settings,
  Terminal,
} from "lucide-react"

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

type NavItem = {
  href: string
  label: string
  icon: typeof LayoutDashboard
}

const NAV_ITEMS: NavItem[] = [
  { href: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { href: "/code-helper", label: "Code Helper", icon: Code2 },
  { href: "/billing", label: "Billing", icon: CreditCard },
  { href: "/api-key", label: "API Key", icon: KeyRound },
  { href: "/settings", label: "Settings", icon: Settings },
]

function isActive(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(href + "/")
}

export function CustomerSidebar() {
  const pathname = usePathname()

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              size="lg"
              render={
                <Link href="/dashboard" className="flex items-center gap-2">
                  <div className="bg-sidebar-primary text-sidebar-primary-foreground flex aspect-square size-8 items-center justify-center rounded-md">
                    <Terminal className="size-4" />
                  </div>
                  <div className="grid flex-1 text-left text-sm leading-tight">
                    <span className="truncate font-semibold tracking-tight">
                      SWE-AF
                    </span>
                    <span className="text-muted-foreground truncate text-xs">
                      Code answers, metered
                    </span>
                  </div>
                </Link>
              }
            />
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {NAV_ITEMS.map((item) => (
                <SidebarMenuItem key={item.href}>
                  <SidebarMenuButton
                    tooltip={item.label}
                    isActive={isActive(pathname, item.href)}
                    render={
                      <Link href={item.href}>
                        <item.icon />
                        <span>{item.label}</span>
                      </Link>
                    }
                  />
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              tooltip="Sign out"
              render={
                <Link href="/sign-out">
                  <LogOut />
                  <span>Sign out</span>
                </Link>
              }
            />
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
    </Sidebar>
  )
}
