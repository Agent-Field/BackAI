// Source of truth for the dashboard navigation.
// Mirrors docs/dashboard-ia.md exactly. Sidebar + ⌘K both read from here.

import {
  Activity,
  Boxes,
  CircleDollarSign,
  Cog,
  CreditCard,
  Database,
  FileBadge,
  Globe,
  HardDrive,
  HomeIcon,
  KeyRound,
  Layers,
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
        id: "webhook-activity",
        label: "Webhook Activity",
        href: "/operate/webhook-activity",
        icon: Activity,
        description: "Recent incoming and outgoing deliveries",
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

/** Look up a nav item by route prefix. Used for breadcrumbs and highlight. */
export function findActiveNav(pathname: string): NavItem | undefined {
  return ALL_NAV_ITEMS.find(
    (item) => pathname === item.href || pathname.startsWith(item.href + "/"),
  )
}

// Suppress unused-import warning when these icons are tree-shaken.
export const _ICON_USED = { Tags, Mail }
