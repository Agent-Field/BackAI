// SPDX-License-Identifier: Apache-2.0

"use client"

import {
  Activity,
  AlertTriangle,
  BarChart3,
  Bot,
  Boxes,
  Briefcase,
  Building2,
  CircleDollarSign,
  Clock,
  Code2,
  Container,
  CreditCard,
  Database,
  Fingerprint,
  Flag,
  FlaskConical,
  Globe2,
  HeartPulse,
  Home,
  Inbox,
  KeyRound,
  Layers,
  type LucideIcon,
  Network,
  Plug,
  Puzzle,
  ReceiptText,
  Repeat,
  Send,
  Server,
  Settings,
  Shield,
  ShieldCheck,
  Skull,
  Terminal,
  Ticket,
  Users,
  Wand2,
  Webhook,
  Wrench,
  Zap,
} from "lucide-react"
import Link from "next/link"
import { usePathname } from "next/navigation"
import * as React from "react"

import { api } from "@/lib/api"
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
//      Anchors mirror the top-bar pills (Home / Inbox / Cost / Health).
//      Playground sits in BUILD (per-agent surface) — not an anchor.
//   2. Activity — observability surfaces.
//   3. People  — customer-care surfaces.
//   4. Build   — agent + module construction surfaces (incl. API explorer
//      + Playground).
//   5. Platform — adapter + setup surfaces.
//
// Items not yet groomed (no page brief) carry `comingSoon: true`. The row
// renders dimmed with a small "v0.2" label so operators can see the
// shape of the platform — flat sidebars hide surface area and make the
// system look smaller than it is.

interface NavItem {
  id: string
  label: string
  href: string
  icon: LucideIcon
  comingSoon?: boolean
  /** Overrides the generic coming-soon tooltip when the backend is NOT
   *  actually ready (e.g. needs an adapter deployed). Keeps the sidebar
   *  honest — "backend ready" must be literally true. */
  comingSoonNote?: string
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
]

const GROUPS: NavGroup[] = [
  {
    id: "activity",
    label: "Activity",
    items: [
      { id: "runs", label: "Runs", href: "/activity/runs", icon: Activity },
      { id: "errors", label: "Errors", href: "/activity/errors", icon: AlertTriangle },
      { id: "logs", label: "Logs", href: "/activity/logs", icon: BarChart3 },
      { id: "traces", label: "Traces", href: "/activity/traces", icon: Network, comingSoon: true, comingSoonNote: "page coming soon; backend needs a traces adapter (e.g. Tempo)" },
      { id: "queue", label: "Queue", href: "/activity/queue", icon: Clock },
      { id: "webhooks", label: "Webhook flow", href: "/activity/webhooks", icon: Webhook },
      { id: "cache", label: "Cache", href: "/activity/cache", icon: Zap, comingSoon: true },
      { id: "notifications", label: "Notifications", href: "/activity/notifications", icon: Send, comingSoon: true, comingSoonNote: "page coming soon; backend needs a notification adapter" },
    ],
  },
  {
    id: "people",
    label: "People",
    items: [
      { id: "tenants", label: "Tenants", href: "/people/tenants", icon: Building2 },
      { id: "users", label: "Users", href: "/people/users", icon: Users },
      { id: "keys", label: "API keys", href: "/people/keys", icon: KeyRound },
      { id: "sessions", label: "Sessions", href: "/people/sessions", icon: Fingerprint },
      { id: "budgets", label: "Budgets", href: "/cost#budgets", icon: ReceiptText },
      { id: "audit", label: "Audit log", href: "/people/audit", icon: Shield },
      { id: "activity-log", label: "Activity log", href: "/people/activity", icon: ShieldCheck },
      { id: "oauth", label: "OAuth connections", href: "/people/oauth", icon: Plug, comingSoon: true },
      { id: "billing", label: "Billing", href: "/people/billing", icon: Ticket, comingSoon: true },
    ],
  },
  {
    id: "build",
    label: "Build",
    items: [
      { id: "agents", label: "Agents", href: "/build/agents", icon: Boxes },
      { id: "reasoners", label: "Reasoners", href: "/build/reasoners", icon: Bot },
      { id: "tools", label: "Tools", href: "/build/tools", icon: Wrench, comingSoon: true },
      { id: "skills", label: "Skills (MCP)", href: "/build/skills", icon: Layers, comingSoon: true },
      { id: "harnesses", label: "Harnesses", href: "/build/harnesses", icon: Briefcase, comingSoon: true },
      { id: "crons", label: "Crons", href: "/build/crons", icon: Repeat, comingSoon: true },
      { id: "sandboxes", label: "Sandboxes", href: "/build/sandboxes", icon: Skull, comingSoon: true },
      { id: "modules", label: "Modules", href: "/build/modules", icon: Puzzle, comingSoon: true },
      { id: "data", label: "Data", href: "/build/data", icon: Database, comingSoon: true },
      { id: "playground", label: "Playground", href: "/build/playground", icon: FlaskConical, comingSoon: true },
      { id: "feature-flags", label: "Feature flags", href: "/build/flags", icon: Flag, comingSoon: true },
      { id: "api", label: "API explorer", href: "/build/api", icon: Code2, comingSoon: true },
      { id: "shipwright", label: "Shipwright", href: "/build/shipwright", icon: Wand2, comingSoon: true },
    ],
  },
  {
    id: "platform",
    label: "Platform",
    items: [
      { id: "adapters", label: "Adapters", href: "/platform/adapters", icon: Server },
      { id: "billing", label: "Billing", href: "/platform/billing", icon: CreditCard },
      { id: "secrets", label: "Secrets", href: "/platform/secrets", icon: KeyRound, comingSoon: true },
      { id: "features", label: "Features", href: "/platform/features", icon: Globe2, comingSoon: true },
      { id: "settings", label: "Settings", href: "/platform/settings", icon: Settings, comingSoon: true },
      { id: "containers", label: "Containers", href: "/platform/containers", icon: Container, comingSoon: true },
    ],
  },
]

// Nav items whose whole purpose is billing. Hidden whenever billing is off
// (personal mode, or the billing module disabled in SaaS).
const BILLING_ITEM_IDS = new Set(["billing", "budgets"])

// Nav items whose whole purpose is auth / multi-tenancy / API-key management.
// In personal mode there are no tenants, logins, or API keys to manage, so
// these are hidden — leaving a clean panel for monitoring and verifying
// (Home, Cost, Health, Runs, Errors, Logs, Audit, Activity, Build surfaces).
const AUTH_ITEM_IDS = new Set(["tenants", "users", "keys", "sessions", "oauth"])

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const pathname = usePathname()
  // The sidebar mirrors what the runtime actually enforces, read once from
  // GET /api/v1/modules. Defaults keep every surface visible so a failed read
  // never hides anything in a real SaaS deployment.
  const [billingOff, setBillingOff] = React.useState(false)
  const [personal, setPersonal] = React.useState(false)
  React.useEffect(() => {
    let alive = true
    api
      .modulesState()
      .then((state) => {
        if (!alive) return
        setBillingOff(!state.billing_enabled)
        setPersonal(state.mode === "personal")
      })
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [])

  const groups = React.useMemo(() => {
    if (!billingOff && !personal) return GROUPS
    return GROUPS.map((g) => ({
      ...g,
      items: g.items.filter((i) => {
        if (billingOff && BILLING_ITEM_IDS.has(i.id)) return false
        if (personal && AUTH_ITEM_IDS.has(i.id)) return false
        return true
      }),
      // Drop a group that ends up empty (e.g. People with everything hidden).
    })).filter((g) => g.items.length > 0)
  }, [billingOff, personal])

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
                  {personal ? "Personal mode · monitor only" : "Operator console"}
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
                <NavLink
                  key={item.id}
                  item={item}
                  active={isActive(pathname, item.href)}
                />
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        {groups.map((group) => (
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
        <div className="px-row-x py-row-y flex items-center justify-between">
          <span className="text-meta text-muted-foreground">
            v0.1 · feat/ui-redesign
          </span>
          {/* Plain anchor: /sign-out is a route handler that kills the
              better-auth session server-side and redirects to /login. */}
          <a
            href="/sign-out"
            className="text-meta text-muted-foreground hover:text-foreground"
          >
            Sign out
          </a>
        </div>
      </SidebarFooter>
    </Sidebar>
  )
}

function NavLink({ item, active }: { item: NavItem; active: boolean }) {
  const Icon = item.icon

  // D1 — coming-soon items keep their place so operators can see the
  // shape of the platform (per journeys-v1.md §IA), but they are NOT
  // links. Routing every stub to Home meant clicking "Agents" silently
  // dumped you on the dashboard — which reads as broken, not roadmap.
  // Render them as inert dimmed rows with a quiet "soon" tag instead:
  // nothing clickable misleads, nothing navigates nowhere.
  if (item.comingSoon) {
    return (
      <SidebarMenuItem>
        <SidebarMenuButton
          size="sm"
          disabled
          aria-disabled
          className="gap-inline opacity-50"
          title={`${item.label} — ${item.comingSoonNote ?? "coming soon"}`}
        >
          <Icon className="size-icon-inline" aria-hidden />
          <span className="flex-1 truncate">{item.label}</span>
          <span className="text-meta text-muted-foreground tabular-nums">
            soon
          </span>
        </SidebarMenuButton>
      </SidebarMenuItem>
    )
  }

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        render={<Link href={item.href} />}
        isActive={active}
        size="sm"
        className="gap-inline"
        title={item.label}
      >
        <Icon className="size-icon-inline" aria-hidden />
        <span className="flex-1 truncate">{item.label}</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  )
}

function isActive(pathname: string, href: string): boolean {
  if (href === "/") return pathname === "/"
  if (href.includes("#")) return false
  return pathname === href || pathname.startsWith(href + "/")
}
