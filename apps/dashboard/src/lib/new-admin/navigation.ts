// SPDX-License-Identifier: Apache-2.0

export type NavIcon =
  | "activity"
  | "alert"
  | "archive"
  | "badge"
  | "bot"
  | "box"
  | "brand"
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

export type PageArchetype =
  | "command-center"
  | "split-debugger"
  | "analytics"
  | "delivery-inbox"
  | "approval-queue"
  | "timeline"
  | "topology"
  | "log-console"
  | "registry-detail"
  | "workbench"
  | "data-explorer"
  | "customer-drilldown"
  | "config-inventory"
  | "read-only-file"

export type DataTruth = "backed" | "derived" | "missing" | "degraded" | "backed-conditional"

export type NavItem = {
  title: string
  href: string
  icon: NavIcon
  description: string
  archetype: PageArchetype
  dataTruth: DataTruth
  apiGap?: string
  adapter?: string
  live?: boolean
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
        description: "Developer welcome, live KPIs, recent activity, and service status.",
        archetype: "command-center",
        dataTruth: "backed",
        adapter: "BackAI runtime",
        live: true,
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
        description: "Inspect every agent and handler execution.",
        archetype: "split-debugger",
        dataTruth: "backed",
        adapter: "AgentField",
        live: true,
      },
      {
        title: "Cost",
        href: "/operate/cost",
        icon: "coins",
        description: "Spend, budget pressure, forecast, and cache value.",
        archetype: "analytics",
        dataTruth: "derived",
        apiGap: "Forecast, cache savings, and reasoner grouping are computed client-side.",
        adapter: "LLM gateway",
        live: true,
      },
      {
        title: "Errors",
        href: "/operate/errors",
        icon: "alert",
        description: "Failure triage across runs, jobs, handlers, and deliveries.",
        archetype: "split-debugger",
        dataTruth: "derived",
        apiGap: "Dedicated grouped errors endpoint is missing; grouping is derived from logs.",
        live: true,
      },
      {
        title: "Traces",
        href: "/operate/traces",
        icon: "network",
        description: "Span tree and trace search for request performance debugging.",
        archetype: "split-debugger",
        dataTruth: "degraded",
        apiGap: "In-product trace browser endpoint is missing; deep exploration links to the trace adapter.",
        adapter: "OTel",
      },
      {
        title: "Queue",
        href: "/operate/queue",
        icon: "clock",
        description: "Async job queue health, retries, latency, and dead-letter pressure.",
        archetype: "split-debugger",
        dataTruth: "backed",
        adapter: "River",
        live: true,
      },
      {
        title: "Cache",
        href: "/operate/cache",
        icon: "cache",
        description: "LLM cache effectiveness, hit rate, saved cost, and misses.",
        archetype: "analytics",
        dataTruth: "backed",
        adapter: "LLM cache",
      },
      {
        title: "Sandbox runs",
        href: "/operate/sandbox-runs",
        icon: "terminal",
        description: "Execution log for every sandbox run.",
        archetype: "log-console",
        dataTruth: "backed",
        adapter: "Sandbox",
      },
      {
        title: "Webhook deliveries",
        href: "/operate/webhooks",
        icon: "webhook",
        description: "Outbound webhook deliveries, retries, request and response payloads.",
        archetype: "delivery-inbox",
        dataTruth: "backed",
        adapter: "Svix",
      },
      {
        title: "Notifications",
        href: "/operate/notifications",
        icon: "mail",
        description: "Email, Slack, SMS, and log delivery audit.",
        archetype: "delivery-inbox",
        dataTruth: "backed",
        adapter: "Notifications",
      },
      {
        title: "Approvals",
        href: "/operate/approvals",
        icon: "shield",
        description: "Human-in-the-loop decisions blocking workflow execution.",
        archetype: "approval-queue",
        dataTruth: "backed",
        live: true,
      },
      {
        title: "Activity",
        href: "/operate/activity",
        icon: "activity",
        description: "Customer-side actions and related operational impact.",
        archetype: "timeline",
        dataTruth: "backed",
      },
      {
        title: "Health",
        href: "/operate/health",
        icon: "gauge",
        description: "Runtime and backing service status.",
        archetype: "topology",
        dataTruth: "backed",
      },
      {
        title: "Logs",
        href: "/operate/logs",
        icon: "terminal",
        description: "Raw log viewer with scale-safe filtering and tail mode.",
        archetype: "log-console",
        dataTruth: "backed",
        live: true,
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
        description: "Agent registry, reasoners, recent runs, and test playground.",
        archetype: "registry-detail",
        dataTruth: "backed",
        adapter: "AgentField",
      },
      {
        title: "Reasoners",
        href: "/build/reasoners",
        icon: "network",
        description: "Flat listing of reasoning steps across agents.",
        archetype: "registry-detail",
        dataTruth: "derived",
        apiGap: "Cross-agent cost and latency analytics are deferred.",
      },
      {
        title: "Tools",
        href: "/build/tools",
        icon: "plug",
        description: "Native and MCP tool inventory with invoke surfaces.",
        archetype: "registry-detail",
        dataTruth: "derived",
        apiGap: "Usage analytics are deferred.",
      },
      {
        title: "Skills",
        href: "/build/skills",
        icon: "spark",
        description: "MCP servers, installed skills, exposed tools, and attachments.",
        archetype: "registry-detail",
        dataTruth: "backed",
      },
      {
        title: "Harnesses",
        href: "/build/harnesses",
        icon: "code",
        description: "Claude Code, Codex, Gemini, and opencode probe status.",
        archetype: "registry-detail",
        dataTruth: "backed",
      },
      {
        title: "Crons",
        href: "/build/crons",
        icon: "clock",
        description: "Scheduled jobs, next runs, active state, and history.",
        archetype: "registry-detail",
        dataTruth: "backed",
      },
      {
        title: "Sandboxes",
        href: "/build/sandboxes",
        icon: "terminal",
        description: "Command workbench and sandbox pool status.",
        archetype: "workbench",
        dataTruth: "backed",
        adapter: "Sandbox",
      },
      {
        title: "Modules",
        href: "/build/modules",
        icon: "plug",
        description: "Workload modules, routes, migrations, and source paths.",
        archetype: "registry-detail",
        dataTruth: "backed",
      },
      {
        title: "Data / Tables",
        href: "/build/data/tables",
        icon: "table",
        description: "Postgres tables, row counts, structure, policies, and preview rows.",
        archetype: "data-explorer",
        dataTruth: "backed",
        adapter: "Postgres",
      },
      {
        title: "Data / SQL",
        href: "/build/data/sql",
        icon: "database",
        description: "Read-only SQL workbench.",
        archetype: "workbench",
        dataTruth: "backed",
        adapter: "Postgres",
      },
      {
        title: "Data / Memory",
        href: "/build/data/memory",
        icon: "archive",
        description: "Vector memory scopes, keys, values, and semantic search.",
        archetype: "data-explorer",
        dataTruth: "backed",
        adapter: "AgentField memory",
      },
      {
        title: "Data / Storage",
        href: "/build/data/storage",
        icon: "box",
        description: "Object browser, metadata, signed URLs, and usage.",
        archetype: "data-explorer",
        dataTruth: "backed",
        adapter: "S3",
      },
      {
        title: "Data / Search",
        href: "/build/data/search",
        icon: "search",
        description: "Search indexes and query probes.",
        archetype: "workbench",
        dataTruth: "backed",
      },
      {
        title: "Feature flags",
        href: "/build/feature-flags",
        icon: "flag",
        description: "Runtime flags, rollout state, and audit-aware edits.",
        archetype: "registry-detail",
        dataTruth: "backed",
      },
      {
        title: "API explorer",
        href: "/build/api-explorer",
        icon: "rocket",
        description: "Try the running fork's OpenAPI endpoints with active auth.",
        archetype: "workbench",
        dataTruth: "backed",
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
        description: "Customer workspaces, cost, status, members, keys, and drilldown.",
        archetype: "customer-drilldown",
        dataTruth: "backed",
      },
      {
        title: "API keys",
        href: "/customers/api-keys",
        icon: "key",
        description: "Tenant keys, spend, limits, rotation, and revocation.",
        archetype: "customer-drilldown",
        dataTruth: "backed",
      },
      {
        title: "Members",
        href: "/customers/members",
        icon: "users",
        description: "Users, memberships, security actions, export, and erase.",
        archetype: "customer-drilldown",
        dataTruth: "backed",
        adapter: "Auth",
      },
      {
        title: "Sessions",
        href: "/customers/sessions",
        icon: "lock",
        description: "Active customer sessions and auth events.",
        archetype: "customer-drilldown",
        dataTruth: "degraded",
        apiGap: "Adapter session enumeration capability is not universal yet.",
        adapter: "Auth",
      },
      {
        title: "Budgets",
        href: "/customers/budgets",
        icon: "gauge",
        description: "Tenant caps, thresholds, usage, and alert status.",
        archetype: "analytics",
        dataTruth: "backed",
      },
      {
        title: "Audit log",
        href: "/customers/audit",
        icon: "receipt",
        description: "Every operator and system mutation with provenance.",
        archetype: "timeline",
        dataTruth: "backed",
      },
      {
        title: "OAuth connections",
        href: "/customers/oauth",
        icon: "link",
        description: "Per-tenant external OAuth grants and provider status.",
        archetype: "customer-drilldown",
        dataTruth: "backed",
        apiGap: "Refresh history endpoint is missing.",
      },
      {
        title: "Billing summary",
        href: "/customers/billing",
        icon: "badge",
        description: "Per-tenant billing records, meters, and portal links.",
        archetype: "analytics",
        dataTruth: "backed",
        adapter: "Billing",
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
        description: "Every backend slot, current adapter, and capability caveat.",
        archetype: "topology",
        dataTruth: "backed",
      },
      {
        title: "Auth providers",
        href: "/setup/auth-providers",
        icon: "lock",
        description: "Auth provider config, capabilities, and secret readiness.",
        archetype: "config-inventory",
        dataTruth: "degraded",
        apiGap: "Runtime auth adapter capability endpoint is pending.",
        adapter: "Auth",
      },
      {
        title: "LLM providers",
        href: "/setup/llm-providers",
        icon: "spark",
        description: "Gateway models, provider key health, and adapter metadata.",
        archetype: "config-inventory",
        dataTruth: "degraded",
        apiGap: "Gateway adapter capabilities are pending.",
        adapter: "LLM gateway",
      },
      {
        title: "Sandbox adapter",
        href: "/setup/sandbox",
        icon: "server",
        description: "Current sandbox adapter and pool status.",
        archetype: "config-inventory",
        dataTruth: "backed",
        adapter: "Sandbox",
      },
      {
        title: "Webhook subscribers",
        href: "/setup/webhook-subscribers",
        icon: "webhook",
        description: "Outbound endpoints, signing, retries, and event types.",
        archetype: "config-inventory",
        dataTruth: "backed",
        adapter: "Svix",
      },
      {
        title: "Notifications",
        href: "/setup/notifications",
        icon: "mail",
        description: "Channel configuration and test send.",
        archetype: "config-inventory",
        dataTruth: "degraded",
        apiGap: "Channel CRUD endpoint is deferred.",
        adapter: "Notifications",
      },
      {
        title: "Secrets",
        href: "/setup/secrets",
        icon: "shield",
        description: "Vault keys, rotation windows, and reveal controls.",
        archetype: "config-inventory",
        dataTruth: "backed",
        adapter: "Secrets",
      },
      {
        title: "Observability",
        href: "/setup/observability",
        icon: "activity",
        description: "Metrics, logs, traces, exporters, and runtime-owned metadata.",
        archetype: "config-inventory",
        dataTruth: "derived",
        apiGap: "Runtime config writes are env-only in v1.",
      },
      {
        title: "Billing adapter",
        href: "/setup/billing-adapter",
        icon: "coins",
        description: "Stripe or Lago adapter readiness and plan mapping.",
        archetype: "config-inventory",
        dataTruth: "derived",
        apiGap: "Runtime adapter switch is env-only in v1.",
        adapter: "Billing",
      },
      {
        title: "Deploy targets",
        href: "/setup/deploy-targets",
        icon: "globe",
        description: "Informational deploy target status and provider metadata.",
        archetype: "config-inventory",
        dataTruth: "missing",
        apiGap: "No runtime provisioning or deploy-status endpoint exists in v1.",
      },
    ],
  },
]

export const pinnedNavItems: NavItem[] = [
  {
    title: "Brand",
    href: "/brand",
    icon: "brand",
    description: "Structured brand.yaml display, DB override, and public identity tokens.",
    archetype: "config-inventory",
    dataTruth: "backed",
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
