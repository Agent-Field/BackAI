// Source of truth for the dashboard navigation.
// Mirrors docs/dashboard-ia.md exactly. Sidebar + ⌘K both read from here.
//
// Plugins extend nav at render time via `getNavGroupsWithPlugins()` /
// `getSettingsNavWithPlugins()` / `getAllNavItemsWithPlugins()`. The static
// `NAV_GROUPS`, `NAV_HOME`, `NAV_SETTINGS`, and `ALL_NAV_ITEMS` exports remain
// for any caller that wants the shell-only view.

import { loadPlugins } from "./plugins"
import {
  Activity,
  Bot,
  Boxes,
  CircleDollarSign,
  Clock,
  Cog,
  CreditCard,
  Database,
  FileBadge,
  Globe,
  HardDrive,
  HomeIcon,
  KeyRound,
  Layers,
  LineChart,
  ListChecks,
  Lock,
  type LucideIcon,
  Mail,
  PlugZap,
  ReceiptText,
  ScrollText,
  ShieldCheck,
  Tags,
  TerminalSquare,
  Timer,
  Users,
  Webhook,
  Workflow,
  Wrench,
} from "lucide-react"

export type NavItem = {
  id: string
  label: string
  href: string
  icon: LucideIcon
  /** Description used in ⌘K palette. */
  description?: string
  /**
   * When true, the tab still renders but its content shows an
   * "enable multi-tenancy" empty state when MT module is disabled.
   * See docs/dashboard-ia.md.
   */
  requiresMultiTenancy?: boolean
}

export type NavGroup = {
  id: "build" | "operate" | "customers" | "system"
  label: string
  items: NavItem[]
}

export const NAV_HOME: NavItem = {
  id: "home",
  label: "Home",
  href: "/",
  icon: HomeIcon,
  description: "Overview, recent activity, alerts",
}

export const NAV_SETTINGS: NavItem = {
  id: "settings",
  label: "Settings",
  href: "/settings",
  icon: Cog,
  description: "Operator account, theme, plugins, feature flags",
}

export const NAV_GROUPS: NavGroup[] = [
  {
    id: "build",
    label: "Build",
    items: [
      {
        id: "agents",
        label: "Agents",
        href: "/build/agents",
        icon: Boxes,
        description: "Registered agents, schemas, sample inputs",
      },
      {
        id: "integrations",
        label: "Integrations",
        href: "/build/integrations",
        icon: PlugZap,
        description: "Tools, MCP servers, skills, harnesses",
      },
      {
        id: "database",
        label: "Database",
        href: "/build/database",
        icon: Database,
        description: "Tables, SQL runner, RLS policies, vector collections",
      },
      {
        id: "storage",
        label: "Storage",
        href: "/build/storage",
        icon: HardDrive,
        description: "Buckets and signed URL configuration",
      },
      {
        id: "secrets",
        label: "Secrets",
        href: "/build/secrets",
        icon: Lock,
        description: "Per-tenant secrets vault",
      },
      {
        id: "webhooks",
        label: "Webhooks",
        href: "/build/webhooks",
        icon: Webhook,
        description: "Incoming and outgoing webhook endpoints",
      },
      {
        id: "auth",
        label: "Auth",
        href: "/build/auth",
        icon: ShieldCheck,
        description: "Identity providers, sessions, MFA",
      },
      {
        id: "billing",
        label: "Billing",
        href: "/build/billing",
        icon: CreditCard,
        description: "Billing your customers see — plans, metered metrics",
      },
      {
        id: "jobs",
        label: "Jobs",
        href: "/build/jobs",
        icon: Timer,
        description: "Background job definitions and cron schedules",
      },
      {
        id: "sandboxes",
        label: "Sandboxes",
        href: "/build/sandboxes",
        icon: TerminalSquare,
        description: "Sandbox adapter configuration",
      },
      {
        id: "mcp",
        label: "MCP",
        href: "/build/mcp",
        icon: Wrench,
        description: "Model Context Protocol servers and tools",
      },
      {
        id: "skills",
        label: "Skills",
        href: "/build/skills",
        icon: Layers,
        description: "Installed AF skillkit bundles — install, list, attach",
      },
      {
        id: "harnesses",
        label: "Harnesses",
        href: "/build/harnesses",
        icon: Bot,
        description: "CLI agent harnesses (Claude Code, Codex, Gemini, OpenCode)",
      },
      {
        id: "modules",
        label: "Modules",
        href: "/build/modules",
        icon: Layers,
        description: "Read-only view of config.yaml",
      },
    ],
  },
  {
    id: "operate",
    label: "Operate",
    items: [
      {
        id: "runs",
        label: "Runs",
        href: "/operate/runs",
        icon: Workflow,
        description: "Agent executions with logs and traces",
      },
      {
        id: "logs",
        label: "Logs",
        href: "/operate/logs",
        icon: ScrollText,
        description: "Live tail across all services",
      },
      {
        id: "queues",
        label: "Queues",
        href: "/operate/queues",
        icon: ListChecks,
        description: "Live job queue state",
      },
      {
        id: "cost",
        label: "Cost",
        href: "/operate/cost",
        icon: CircleDollarSign,
        description: "Spend by model, agent, tenant, day",
      },
      {
        id: "sandbox-activity",
        label: "Sandbox Activity",
        href: "/operate/sandbox-activity",
        icon: Activity,
        description: "Live pool and recent sandbox runs",
      },
      {
        id: "notifications",
        label: "Notifications",
        href: "/operate/notifications",
        icon: Mail,
        description: "Outbox + recent deliveries (email, SMS, push)",
      },
      {
        id: "webhook-activity",
        label: "Webhook Activity",
        href: "/operate/webhook-activity",
        icon: Activity,
        description: "Recent incoming and outgoing deliveries",
      },
      {
        id: "crons",
        label: "Crons",
        href: "/operate/crons",
        icon: Clock,
        description: "Scheduled jobs that fire on a cron expression",
      },
      {
        id: "metrics",
        label: "Metrics",
        href: "/operate/metrics",
        icon: LineChart,
        description: "At-a-glance runtime metrics — requests, p95, heap",
      },
    ],
  },
  {
    id: "customers",
    label: "Customers",
    items: [
      {
        id: "tenants",
        label: "Tenants",
        href: "/customers/tenants",
        icon: Globe,
        description: "Your customer orgs",
        requiresMultiTenancy: true,
      },
      {
        id: "users",
        label: "Users",
        href: "/customers/users",
        icon: Users,
        description: "End users across tenants",
        requiresMultiTenancy: true,
      },
      {
        id: "api-keys",
        label: "API Keys",
        href: "/customers/api-keys",
        icon: KeyRound,
        description: "Keys issued to your customers",
        requiresMultiTenancy: true,
      },
      {
        id: "customer-billing",
        label: "Customer Billing",
        href: "/customers/customer-billing",
        icon: ReceiptText,
        description: "Per-tenant billing and invoices",
        requiresMultiTenancy: true,
      },
      {
        id: "audit",
        label: "Audit",
        href: "/customers/audit",
        icon: FileBadge,
        description: "Who did what when",
        requiresMultiTenancy: true,
      },
    ],
  },
]

/** Flat list of every nav item, including Home and Settings. */
export const ALL_NAV_ITEMS: NavItem[] = [
  NAV_HOME,
  ...NAV_GROUPS.flatMap((g) => g.items),
  NAV_SETTINGS,
]

/**
 * Returns the four primary nav groups (build / operate / customers + an extra
 * `system` group for plugins) with plugin items merged into the group each
 * plugin requested. Pure: safe to call from server or client components.
 *
 * - Plugins whose `group` is `build|operate|customers` are appended to the
 *   matching static group's items.
 * - Plugins whose `group` is `system` form a new "System" group that is only
 *   returned when at least one plugin uses it (otherwise the sidebar keeps the
 *   default shape).
 */
export function getNavGroupsWithPlugins(): NavGroup[] {
  const plugins = loadPlugins()
  if (plugins.length === 0) return NAV_GROUPS

  const groups = NAV_GROUPS.map((g) => ({ ...g, items: [...g.items] }))
  const systemItems: NavItem[] = []

  for (const plugin of plugins) {
    const item: NavItem = {
      id: `plugin-${plugin.id}`,
      label: plugin.label,
      href: plugin.href,
      icon: plugin.icon,
      description: plugin.description,
    }
    if (plugin.group === "system") {
      systemItems.push(item)
      continue
    }
    const target = groups.find((g) => g.id === plugin.group)
    if (target) target.items.push(item)
  }

  if (systemItems.length > 0) {
    groups.push({ id: "system", label: "System", items: systemItems })
  }
  return groups
}

/** Flat list including plugin nav items. Used by ⌘K to find anything. */
export function getAllNavItemsWithPlugins(): NavItem[] {
  return [
    NAV_HOME,
    ...getNavGroupsWithPlugins().flatMap((g) => g.items),
    NAV_SETTINGS,
  ]
}

/** Look up a nav item by route prefix. Used for breadcrumbs and highlight. */
export function findActiveNav(pathname: string): NavItem | undefined {
  return getAllNavItemsWithPlugins().find(
    (item) => pathname === item.href || pathname.startsWith(item.href + "/"),
  )
}

// Suppress unused-import warning when these icons are tree-shaken.
export const _ICON_USED = { Tags, Mail }
