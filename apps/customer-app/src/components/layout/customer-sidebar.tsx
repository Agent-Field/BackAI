// SPDX-License-Identifier: Apache-2.0

"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import {
  CreditCard,
  KeyRound,
  LayoutDashboard,
  LogOut,
  MessageSquareText,
  Plug,
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
import { brand } from "@/lib/brand"

type NavItem = {
  href: string
  label: string
  icon: typeof LayoutDashboard
}

const NAV_ITEMS: NavItem[] = [
  { href: "/dashboard", label: "Workspace", icon: LayoutDashboard },
  { href: "/code-helper", label: "Support Desk", icon: MessageSquareText },
  { href: "/integrations", label: "Integrations", icon: Plug },
  { href: "/billing", label: "Billing", icon: CreditCard },
  { href: "/api-key", label: "API Key", icon: KeyRound },
  { href: "/settings", label: "Settings", icon: Settings },
]

function isActive(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(href + "/")
}

export function CustomerSidebar({ billingDisabled = false }: { billingDisabled?: boolean }) {
  const pathname = usePathname()
  const navItems = billingDisabled
    ? NAV_ITEMS.filter((item) => item.href !== "/billing")
    : NAV_ITEMS

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
                    {brand.logos.light ? (
                      <img src={brand.logos.light} alt="" className="size-5 object-contain" />
                    ) : (
                      <Terminal className="size-4" />
                    )}
                  </div>
                  <div className="grid flex-1 text-left text-sm leading-tight">
                    <span className="truncate font-semibold tracking-tight">
                      {brand.displayName}
                    </span>
                    <span className="text-muted-foreground truncate text-xs">{brand.subtitle}</span>
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
              {navItems.map((item) => (
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
