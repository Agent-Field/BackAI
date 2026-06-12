// SPDX-License-Identifier: Apache-2.0

"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { Boxes } from "lucide-react"

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"
import { Badge } from "@/components/ui/badge"
import { brand } from "@/lib/brand"
import { getNavGroupsWithPlugins, NAV_HOME, NAV_SETTINGS, type NavItem } from "@/lib/nav"

function isActive(pathname: string, href: string): boolean {
  if (href === "/") return pathname === "/"
  return pathname === href || pathname.startsWith(href + "/")
}

function NavLink({ item, pathname }: { item: NavItem; pathname: string }) {
  return (
    <SidebarMenuButton
      tooltip={item.label}
      isActive={isActive(pathname, item.href)}
      render={
        <Link
          href={item.href}
          data-tour={
            item.id === "agents"
              ? "admin-agent-nav"
              : item.id === "cost"
                ? "admin-cost-nav"
                : undefined
          }
        >
          <item.icon />
          <span>{item.label}</span>
          {item.requiresMultiTenancy ? (
            <Badge variant="outline" className="ml-auto text-[10px] font-normal">
              MT
            </Badge>
          ) : null}
        </Link>
      }
    />
  )
}

export function AppSidebar({
  billingDisabled = false,
  showShipwright = false,
}: {
  billingDisabled?: boolean
  showShipwright?: boolean
}) {
  const pathname = usePathname()
  const navGroups = getNavGroupsWithPlugins({ billingDisabled, showShipwright })

  return (
    <Sidebar collapsible="icon" variant="inset">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              size="lg"
              render={
                <Link href="/" className="flex items-center gap-2">
                  <div className="bg-sidebar-primary text-sidebar-primary-foreground flex aspect-square size-8 items-center justify-center rounded-md">
                    {brand.logos.light ? (
                      <img src={brand.logos.light} alt="" className="size-5 object-contain" />
                    ) : (
                      <Boxes className="size-4" />
                    )}
                  </div>
                  <div className="grid flex-1 text-left text-sm leading-tight">
                    <span className="truncate font-semibold">{brand.displayName}</span>
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
              <SidebarMenuItem>
                <NavLink item={NAV_HOME} pathname={pathname} />
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        {navGroups.map((group) => (
          <SidebarGroup key={group.id}>
            <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {group.items.map((item) => (
                  <SidebarMenuItem key={item.id}>
                    <NavLink item={item} pathname={pathname} />
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>

      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <NavLink item={NAV_SETTINGS} pathname={pathname} />
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
    </Sidebar>
  )
}
