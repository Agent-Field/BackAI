// SPDX-License-Identifier: Apache-2.0

"use client"

import {
  Activity,
  AlertTriangle,
  BarChart3,
  Boxes,
  Briefcase,
  Building2,
  CircleDollarSign,
  Clock,
  Code2,
  Database,
  FileText,
  Globe2,
  HeartPulse,
  Home,
  Inbox,
  KeyRound,
  Layers,
  type LucideIcon,
  Plug,
  Puzzle,
  ReceiptText,
  Send,
  Server,
  Settings,
  Shield,
  Skull,
  Sparkles,
  Terminal,
  Users,
  Webhook,
} from "lucide-react"
import Link from "next/link"
import { usePathname } from "next/navigation"
import * as React from "react"

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

// IA derived from development/ux/journeys-v1.md §"What this implies for
// UX/UI organization". Five pillars:
//   1. Anchor — top-pinned, ungrouped: the surfaces every operator hits.
//   2. Activity — observability surfaces.
//   3. People  — customer-care surfaces.
//   4. Build   — agent + module construction surfaces.
//   5. Platform — adapter + setup surfaces.
//
// Destinations are stubbed at "/" or "#" for surfaces whose page briefs
// haven't been groomed yet — clicks render Home until those pages ship.

interface NavItem {
  id: string
  label: string
  href: string
  icon: LucideIcon
}

interface NavGroup {
  id: string
  label: string
  items: NavItem[]
}

const ANCHORS: NavItem[] = [
  { id: "home", label: "Home", href: "/", icon: Home },
  { id: "inbox", label: "Inbox", href: "/inbox", icon: Inbox },
  { id: "cost", label: "Cost", href: "/cost", icon: CircleDollarSign },
  { id: "health", label: "Health", href: "/health", icon: HeartPulse },
  { id: "playground", label: "Playground", href: "/playground", icon: Sparkles },
]

const GROUPS: NavGroup[] = [
  {
    id: "activity",
    label: "Activity",
    items: [
      { id: "runs", label: "Runs", href: "/activity/runs", icon: Activity },
      { id: "errors", label: "Errors", href: "/activity/errors", icon: AlertTriangle },
      { id: "logs", label: "Logs", href: "/activity/logs", icon: FileText },
      { id: "traces", label: "Traces", href: "/activity/traces", icon: BarChart3 },
      { id: "queue", label: "Queue", href: "/activity/queue", icon: Clock },
      { id: "webhooks", label: "Webhooks", href: "/activity/webhooks", icon: Webhook },
      { id: "notifications", label: "Notifications", href: "/activity/notifications", icon: Send },
    ],
  },
  {
    id: "people",
    label: "People",
    items: [
      { id: "tenants", label: "Tenants", href: "/people/tenants", icon: Building2 },
      { id: "users", label: "Users", href: "/people/users", icon: Users },
      { id: "keys", label: "API keys", href: "/people/keys", icon: KeyRound },
      { id: "billing", label: "Billing", href: "/people/billing", icon: ReceiptText },
      { id: "audit", label: "Audit log", href: "/people/audit", icon: Shield },
    ],
  },
  {
    id: "build",
    label: "Build",
    items: [
      { id: "agents", label: "Agents", href: "/build/agents", icon: Boxes },
      { id: "modules", label: "Modules", href: "/build/modules", icon: Puzzle },
      { id: "integrations", label: "Integrations", href: "/build/integrations", icon: Plug },
      { id: "skills", label: "Skills", href: "/build/skills", icon: Layers },
      { id: "data", label: "Data", href: "/build/data", icon: Database },
      { id: "storage", label: "Storage", href: "/build/storage", icon: Server },
      { id: "sandboxes", label: "Sandboxes", href: "/build/sandboxes", icon: Skull },
    ],
  },
  {
    id: "platform",
    label: "Platform",
    items: [
      { id: "adapters", label: "Adapters", href: "/platform/adapters", icon: Briefcase },
      { id: "secrets", label: "Secrets", href: "/platform/secrets", icon: KeyRound },
      { id: "features", label: "Features", href: "/platform/features", icon: Globe2 },
      { id: "settings", label: "Settings", href: "/platform/settings", icon: Settings },
      { id: "api", label: "API explorer", href: "/platform/api", icon: Code2 },
    ],
  },
]

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const pathname = usePathname()
  return (
    <Sidebar variant="sidebar" {...props}>
      <SidebarHeader className="px-row-x py-stack">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              size="lg"
              render={<Link href="/" />}
              className="gap-inline"
            >
              <span className="inline-flex size-7 items-center justify-center rounded-md bg-sidebar-primary text-sidebar-primary-foreground">
                <Terminal className="size-4" aria-hidden />
              </span>
              <div className="flex min-w-0 flex-col leading-tight">
                <span className="truncate text-body font-semibold">BackAI</span>
                <span className="truncate text-meta text-muted-foreground">
                  Operator console
                </span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {ANCHORS.map((item) => (
                <NavLink key={item.id} item={item} active={isActive(pathname, item.href)} />
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        {GROUPS.map((group) => (
          <SidebarGroup key={group.id}>
            <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {group.items.map((item) => (
                  <NavLink
                    key={item.id}
                    item={item}
                    active={isActive(pathname, item.href)}
                  />
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>
      <SidebarFooter>
        <span className="px-row-x py-row-y text-meta text-muted-foreground">
          v0.1 · feat/ui-redesign
        </span>
      </SidebarFooter>
    </Sidebar>
  )
}

function NavLink({ item, active }: { item: NavItem; active: boolean }) {
  const Icon = item.icon
  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        render={<Link href={item.href} />}
        isActive={active}
        size="sm"
        className="gap-inline"
      >
        <Icon className="size-icon-inline" aria-hidden />
        <span>{item.label}</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  )
}

function isActive(pathname: string, href: string): boolean {
  if (href === "/") return pathname === "/"
  return pathname === href || pathname.startsWith(href + "/")
}
