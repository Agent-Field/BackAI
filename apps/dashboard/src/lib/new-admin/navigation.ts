// SPDX-License-Identifier: Apache-2.0

export type NavIcon =
  | "activity"
  | "alert"
  | "archive"
  | "badge"
  | "book"
  | "bot"
  | "box"
  | "brand"
  | "braces"
  | "building"
  | "cache"
  | "chart"
  | "clock"
  | "code"
  | "coins"
  | "database"
  | "flag"
  | "gauge"
  | "globe"
  | "home"
  | "key"
  | "link"
  | "list"
  | "lock"
  | "mail"
  | "network"
  | "plug"
  | "receipt"
  | "rocket"
  | "search"
  | "server"
  | "settings"
  | "shield"
  | "spark"
  | "table"
  | "terminal"
  | "users"
  | "webhook"

export type NavItem = {
  title: string
  href: string
  icon: NavIcon
  description: string
}

export type NavGroup = {
  title: string
  defaultOpen: boolean
  items: NavItem[]
}

export const navGroups: NavGroup[] = [
  {
    title: "Overview",
    defaultOpen: true,
    items: [
      {
        title: "Home",
        href: "/",
        icon: "home",
        description: "Platform health, live KPIs, activity, and service status.",
      },
    ],
  },
  {
    title: "Develop",
    defaultOpen: true,
    items: [
      {
        title: "Quickstart",
        href: "/develop/quickstart",
        icon: "rocket",
        description: "Runtime URL, tenant key, install commands, and first calls.",
      },
      {
        title: "API explorer",
        href: "/develop/api-explorer",
        icon: "terminal",
        description: "Try endpoints against this fork with the active auth context.",
      },
      {
        title: "SDK & CLI",
        href: "/develop/sdk-cli",
        icon: "code",
        description: "Install snippets and common SDK/CLI calls.",
      },
      {
        title: "Schema / OpenAPI",
        href: "/develop/schema",
        icon: "braces",
        description: "Browse, download, and generate clients from the API schema.",
      },
      {
        title: "Integration recipes",
        href: "/develop/recipes",
        icon: "book",
        description: "Common glue patterns for webhooks, jobs, billing, and agents.",
      },
    ],
  },
  {
    title: "Operate",
    defaultOpen: true,
    items: [
      {
        title: "Runs",
        href: "/operate/runs",
        icon: "list",
        description: "Live agent and handler execution stream with split-pane debug.",
      },
      {
        title: "Cost",
        href: "/operate/cost",
        icon: "coins",
        description: "Spend, forecasts, cache savings, and budget control.",
      },
      {
        title: "Errors",
        href: "/operate/errors",
        icon: "alert",
        description: "Failure triage across runs, jobs, handlers, and webhooks.",
      },
      {
        title: "Traces",
        href: "/operate/traces",
        icon: "network",
        description: "Trace list and span tree for request performance debugging.",
      },
      {
        title: "Queue",
        href: "/operate/queue",
        icon: "clock",
        description: "Async jobs, latency, retries, and dead-letter pressure.",
      },
      {
        title: "Webhook deliveries",
        href: "/operate/webhooks",
        icon: "webhook",
        description: "Inbound and outbound webhook delivery log.",
      },
      {
        title: "Cache",
        href: "/operate/cache",
        icon: "cache",
        description: "LLM cache hit rate, saved cost, and top reusable prompts.",
      },
    ],
  },
  {
    title: "Build",
    defaultOpen: true,
    items: [
      {
        title: "Agents",
        href: "/build/agents",
        icon: "bot",
        description: "Runtime agent catalog, reasoners, versions, and test paths.",
      },
      {
        title: "Data / Tables",
        href: "/build/data/tables",
        icon: "table",
        description: "Database tables, row counts, policies, and recent writes.",
      },
      {
        title: "Data / SQL",
        href: "/build/data/sql",
        icon: "database",
        description: "Read-only SQL workbench and saved operational queries.",
      },
      {
        title: "Data / Memory",
        href: "/build/data/memory",
        icon: "archive",
        description: "Memory scopes, keys, vector coverage, and search samples.",
      },
      {
        title: "Data / Storage",
        href: "/build/data/storage",
        icon: "box",
        description: "Object keys, sizes, signed URLs, and storage adapter status.",
      },
      {
        title: "Data / Search",
        href: "/build/data/search",
        icon: "search",
        description: "Search indexes, ingestion status, and retrieval probes.",
      },
      {
        title: "Modules",
        href: "/build/modules",
        icon: "plug",
        description: "Workload modules and code-owned extension points.",
      },
      {
        title: "Skills (MCP)",
        href: "/build/skills",
        icon: "spark",
        description: "Installed skills, MCP servers, tools, and attachments.",
      },
      {
        title: "Feature flags",
        href: "/build/feature-flags",
        icon: "flag",
        description: "Runtime config flags and rollout state.",
      },
    ],
  },
  {
    title: "Customers",
    defaultOpen: false,
    items: [
      {
        title: "Tenants",
        href: "/customers/tenants",
        icon: "building",
        description: "Customer workspaces, isolation, health, and drilldowns.",
      },
      {
        title: "API keys",
        href: "/customers/api-keys",
        icon: "key",
        description: "Tenant keys, spend, limits, and rotation state.",
      },
      {
        title: "Members",
        href: "/customers/members",
        icon: "users",
        description: "Users and memberships across tenants.",
      },
      {
        title: "Budgets",
        href: "/customers/budgets",
        icon: "gauge",
        description: "Caps, thresholds, usage, and alert status.",
      },
      {
        title: "Audit log",
        href: "/customers/audit",
        icon: "receipt",
        description: "Operator and system mutations with source links.",
      },
      {
        title: "Billing summary",
        href: "/customers/billing",
        icon: "badge",
        description: "Customer billing adapter records and metered usage.",
      },
    ],
  },
  {
    title: "Setup",
    defaultOpen: false,
    items: [
      {
        title: "Adapters",
        href: "/setup/adapters",
        icon: "settings",
        description: "Every swappable backend slot and its current adapter.",
      },
      {
        title: "Auth providers",
        href: "/setup/auth-providers",
        icon: "lock",
        description: "Operator and customer auth providers.",
      },
      {
        title: "LLM providers",
        href: "/setup/llm-providers",
        icon: "spark",
        description: "Models, routing, fallback, and LiteLLM status.",
      },
      {
        title: "Sandbox adapter",
        href: "/setup/sandbox",
        icon: "server",
        description: "Sandbox capacity, limits, and provider probes.",
      },
      {
        title: "Webhook subscribers",
        href: "/setup/webhook-subscribers",
        icon: "webhook",
        description: "Outbound endpoints, signing, retries, and events.",
      },
      {
        title: "Notifications",
        href: "/setup/notifications",
        icon: "mail",
        description: "Outbox adapters, channels, templates, and provider health.",
      },
      {
        title: "Secrets",
        href: "/setup/secrets",
        icon: "shield",
        description: "Vault keys, rotation windows, and tenant overrides.",
      },
      {
        title: "Observability",
        href: "/setup/observability",
        icon: "activity",
        description: "Logs, metrics, traces, and external observability links.",
      },
      {
        title: "Billing adapter",
        href: "/setup/billing-adapter",
        icon: "coins",
        description: "Lago or Stripe adapter readiness and sync state.",
      },
      {
        title: "Deploy targets",
        href: "/setup/deploy-targets",
        icon: "globe",
        description: "Runtime, dashboard, storage, and worker deployment surfaces.",
      },
    ],
  },
]

export const pinnedNavItems: NavItem[] = [
  {
    title: "Brand",
    href: "/brand",
    icon: "brand",
    description: "Operator-owned identity, theme assets, and public naming.",
  },
]

export const allNavItems = [...navGroups.flatMap((group) => group.items), ...pinnedNavItems]

export function normalizePath(pathname: string): string {
  if (pathname === "") return "/"
  if (pathname.length > 1 && pathname.endsWith("/")) return pathname.slice(0, -1)
  return pathname
}

export function navItemForPath(pathname: string): NavItem {
  const normalized = normalizePath(pathname)
  return (
    allNavItems.find((item) => item.href === normalized) ??
    allNavItems.find((item) => normalized.startsWith(`${item.href}/`)) ??
    allNavItems[0]
  )
}

export function groupForPath(pathname: string): string {
  const normalized = normalizePath(pathname)
  for (const group of navGroups) {
    if (group.items.some((item) => item.href === normalized || normalized.startsWith(`${item.href}/`))) {
      return group.title
    }
  }
  return normalized === "/brand" ? "Brand" : "Overview"
}
