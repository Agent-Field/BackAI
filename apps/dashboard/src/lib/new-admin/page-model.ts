// SPDX-License-Identifier: Apache-2.0

import type { ConsoleRow, Kpi, OperatorSnapshot, StatusTone } from "@/lib/new-admin/data"
import {
  allNavItems,
  groupForPath,
  navItemForPath,
  normalizePath,
  type DataTruth,
  type PageArchetype,
} from "@/lib/new-admin/navigation"

export type PageControl =
  | {
      kind: "select"
      label: string
      value?: string
      options: string[]
    }
  | {
      kind: "tabs"
      label: string
      value?: string
      options: string[]
    }
  | {
      kind: "input"
      label: string
      placeholder: string
    }
  | {
      kind: "switch"
      label: string
      value?: "on" | "off"
    }

export type PageCard = {
  title: string
  description: string
  rows?: Array<{
    label: string
    value: string
    tone?: StatusTone
  }>
  code?: string
}

export type PageModel = {
  path: string
  group: string
  title: string
  description: string
  live: boolean
  source: OperatorSnapshot["source"]
  generatedAt: string
  adapter?: string
  dataTruth: DataTruth
  apiGap?: string
  archetype: PageArchetype
  primaryAction: string
  controls: PageControl[]
  kpis: Kpi[]
  tableTitle: string
  tableDescription: string
  tableColumns: [string, string, string, string]
  rows: ConsoleRow[]
  secondaryTitle: string
  secondary: PageCard[]
}

type PageDefinition = {
  primaryAction: string
  controls: (snapshot: OperatorSnapshot) => PageControl[]
  kpis: (snapshot: OperatorSnapshot) => Kpi[]
  columns?: [string, string, string, string]
  rows: (snapshot: OperatorSnapshot) => ConsoleRow[]
  tableTitle: string
  tableDescription: string
  secondaryTitle: string
  secondary: (snapshot: OperatorSnapshot) => PageCard[]
}

const defaultColumns: [string, string, string, string] = ["Object", "Context", "Metric", "Updated"]

function select(label: string, value: string, options: string[]): PageControl {
  return { kind: "select", label, value, options }
}

function tabs(label: string, value: string, options: string[]): PageControl {
  return { kind: "tabs", label, value, options }
}

function input(label: string, placeholder: string): PageControl {
  return { kind: "input", label, placeholder }
}

function switcher(label: string, value: "on" | "off" = "off"): PageControl {
  return { kind: "switch", label, value }
}

function platformControls(search = "Search current page..."): PageControl[] {
  return [
    select("Scope", "Platform", ["Platform", "acme", "beta-labs", "delta-health"]),
    select("Range", "24h", ["15m", "1h", "24h", "7d", "30d"]),
    input("Search", search),
  ]
}

function statusControls(search = "Search objects..."): PageControl[] {
  return [...platformControls(search), tabs("State", "all", ["all", "active", "failed", "pending"])]
}

function setupControls(search = "Search setup..."): PageControl[] {
  return [
    select("Environment", "local", ["local", "staging", "production"]),
    tabs("Readiness", "all", ["all", "ready", "warning", "missing"]),
    input("Search", search),
  ]
}

function kpi(label: string, value: string, detail: string, trend: string, tone: StatusTone = "neutral"): Kpi {
  return { label, value, detail, trend, tone, sparkline: [8, 10, 9, 12, 14, 13, 16, 15] }
}

function pick(snapshot: OperatorSnapshot, indexes: number[]) {
  return indexes.map((index) => snapshot.kpis[index]).filter(Boolean).slice(0, 4)
}

function card(title: string, description: string, rows?: PageCard["rows"], code?: string): PageCard {
  return { title, description, rows, code }
}

function statusCard(model: { dataTruth: DataTruth; apiGap?: string; adapter?: string }, snapshot: OperatorSnapshot): PageCard {
  return card("Data contract", "What this page can claim from the current backend.", [
    { label: "Source", value: snapshot.source === "live" ? "runtime API" : "seeded fallback", tone: snapshot.source === "live" ? "ok" : "warn" },
    { label: "Truth", value: model.dataTruth, tone: model.dataTruth === "backed" ? "ok" : model.dataTruth === "missing" ? "fail" : "warn" },
    { label: "Adapter", value: model.adapter ?? "runtime" },
    ...(model.apiGap ? [{ label: "Gap", value: model.apiGap, tone: "warn" as StatusTone }] : []),
  ])
}

function selectedRows(rows: ConsoleRow[], count = 5): PageCard["rows"] {
  return rows.slice(0, count).map((row) => ({
    label: row.primary,
    value: row.metric,
    tone: row.tone,
  }))
}

function selectedAdapterRows(snapshot: OperatorSnapshot, count = 5): PageCard["rows"] {
  return snapshot.adapters.slice(0, count).map((adapter) => ({
    label: adapter.slot,
    value: adapter.adapter,
    tone: adapter.status,
  }))
}

const definitions: Record<string, PageDefinition> = {
  "/": {
    primaryAction: "Open command center",
    controls: () => platformControls("Find tenant, run, key, or action..."),
    kpis: (snapshot) => pick(snapshot, [0, 1, 2, 4]),
    rows: (snapshot) => snapshot.activity,
    tableTitle: "Recent platform activity",
    tableDescription: "Runs, alerts, tenant changes, budget events, and service signals in time order.",
    secondaryTitle: "System posture",
    secondary: (snapshot) => [
      card("Service status", "Backing services and adapter readiness.", snapshot.services.map((service) => ({
        label: service.name,
        value: `${service.status} · ${service.adapter ?? "service"}`,
        tone: service.status === "healthy" ? "ok" : service.status === "degraded" ? "warn" : "fail",
      }))),
      card("Budget pressure", "Current tenant budget usage.", snapshot.budgets.map((budget) => ({
        label: budget.tenant,
        value: `${budget.used}% of ${budget.cap}`,
        tone: budget.status,
      }))),
    ],
  },
  "/operate/runs": {
    primaryAction: "Test agent",
    controls: () => statusControls("Search run id, agent, or tenant..."),
    kpis: (snapshot) => pick(snapshot, [0, 5, 6, 2]),
    rows: (snapshot) => snapshot.runs,
    tableTitle: "Execution stream",
    tableDescription: "Agent and handler runs with status, tenant context, cost, and timing.",
    secondaryTitle: "Run drilldown",
    secondary: (snapshot) => [
      card("Selected run", "Use row links to share a filtered run view.", selectedRows(snapshot.runs, 4)),
      card("Actions", "Pause, resume, cancel, request approval, or copy input into the agent playground."),
    ],
  },
  "/operate/cost": {
    primaryAction: "Set budget",
    controls: () => [
      select("Tenant", "Platform", ["Platform", "acme", "beta-labs", "delta-health"]),
      select("Range", "30d", ["24h", "7d", "30d", "90d"]),
      tabs("Group", "tenant", ["tenant", "model", "agent", "day"]),
      input("Search", "Find model, tenant, or run..."),
    ],
    kpis: (snapshot) => pick(snapshot, [2, 3, 7, 1]),
    rows: (snapshot) => snapshot.costRows,
    tableTitle: "Spend ledger",
    tableDescription: "Cost events and derived spend views grouped by the selected control.",
    secondaryTitle: "Budget and cache",
    secondary: (snapshot) => [
      card("Budget pressure", "Caps remain visible while the spend ledger updates.", snapshot.budgets.map((budget) => ({
        label: budget.tenant,
        value: `${budget.used}%`,
        tone: budget.status,
      }))),
      card("Cache value", "Savings are labeled derived unless the gateway returns a native aggregate.", selectedRows(snapshot.cache, 3)),
    ],
  },
  "/operate/errors": {
    primaryAction: "Mute error",
    controls: () => statusControls("Search stack, source, tenant, or run..."),
    kpis: () => [kpi("Open groups", "derived", "from error logs", "client", "warn"), kpi("Grouping", "pattern", "client-side", "logs", "warn"), kpi("Source", "logs", "/api/v1/logs", "live", "running"), kpi("Endpoint gap", "1", "admin errors endpoint", "missing", "fail")],
    rows: (snapshot) => snapshot.errors.length ? snapshot.errors : snapshot.logs.filter((row) => row.tone === "fail"),
    tableTitle: "Failure groups",
    tableDescription: "Runtime errors grouped client-side until a dedicated error aggregation endpoint exists.",
    secondaryTitle: "Triage",
    secondary: (snapshot) => [
      card("Muted and resolved", "Use audit-backed mutations when backend grouping lands."),
      card("Raw samples", "Every group links back to the original log row.", selectedRows(snapshot.logs.filter((row) => row.tone === "fail"), 4)),
    ],
  },
  "/operate/traces": {
    primaryAction: "Open trace explorer",
    controls: () => [select("Scope", "Platform", ["Platform", "tenant"]), input("Trace", "Paste trace id or run id..."), tabs("Status", "all", ["all", "slow", "failed", "sampled"])],
    kpis: () => [kpi("Trace endpoint", "missing", "runtime query", "degraded", "warn"), kpi("Span tree", "thin", "from run context", "adapter", "warn"), kpi("Critical path", "external", "Tempo or Honeycomb", "backend", "neutral"), kpi("Copy link", "ready", "share trace search", "url", "ok")],
    rows: (snapshot) => snapshot.traces,
    tableTitle: "Trace search",
    tableDescription: "Thin in-product trace context with external explorer handoff.",
    secondaryTitle: "Span preview",
    secondary: (snapshot) => [
      card("Capability caveat", "Deep span exploration needs a trace endpoint or adapter query capability.", [{ label: "Missing", value: "GET /api/v1/traces", tone: "warn" }]),
      card("Recent trace contexts", "Rows are linked by run id until trace ids are first-class.", selectedRows(snapshot.traces, 4)),
    ],
  },
  "/operate/queue": {
    primaryAction: "Enqueue job",
    controls: () => statusControls("Search job id, kind, tenant, or error..."),
    kpis: (snapshot) => pick(snapshot, [4, 5, 6, 1]),
    rows: (snapshot) => snapshot.queue,
    tableTitle: "Queue pressure",
    tableDescription: "Async jobs, attempts, retry state, and queue latency signals.",
    secondaryTitle: "Job detail",
    secondary: (snapshot) => [
      card("Job definitions", "Registered workers and cron-backed jobs.", selectedRows(snapshot.jobDefinitions, 5)),
      card("Retry policy", "Failed jobs open a drawer with payload, attempts, and retry action."),
    ],
  },
  "/operate/cache": {
    primaryAction: "Open LLM admin",
    controls: () => [select("Tenant", "Platform", ["Platform", "acme", "beta-labs"]), select("Range", "24h", ["24h", "7d", "30d"]), tabs("View", "summary", ["summary", "hits", "misses"])],
    kpis: () => [kpi("Hit rate", "see rows", "from cache stats", "live", "ok"), kpi("Savings", "derived", "gateway estimate", "client", "running"), kpi("Entries", "live", "cache size", "stats", "ok"), kpi("Flush", "hidden", "no endpoint", "gap", "warn")],
    rows: (snapshot) => snapshot.cache,
    tableTitle: "Cache effectiveness",
    tableDescription: "LLM cache hit rate, savings, misses, and entries from the gateway cache stats.",
    secondaryTitle: "Cache policy",
    secondary: (snapshot) => [
      card("Top cache signals", "Rows are backed by `/api/v1/llm/cache/stats`.", selectedRows(snapshot.cache, 3)),
      card("Actions", "Flush controls stay hidden until backend flush endpoints exist.", [{ label: "Gap", value: "flush endpoint missing", tone: "warn" }]),
    ],
  },
  "/operate/sandbox-runs": {
    primaryAction: "Cancel run",
    controls: () => statusControls("Search command, image, tenant, or exit code..."),
    kpis: () => [kpi("Tail", "ready", "logs route", "linked", "running"), kpi("Cancel", "backed", "DELETE run", "guarded", "ok"), kpi("Artifacts", "linked", "storage", "drill", "neutral"), kpi("Pool", "live", "sandbox adapter", "status", "ok")],
    rows: (snapshot) => snapshot.sandbox,
    tableTitle: "Sandbox execution log",
    tableDescription: "Every sandbox command, exit code, duration, cost, and live output link.",
    secondaryTitle: "Run output",
    secondary: (snapshot) => [
      card("Pool status", "Current sandbox adapter and warm capacity.", selectedRows(snapshot.sandbox, 4)),
      card("Log behavior", "Detail drawers tail stdout and stderr, with pause and copy controls."),
    ],
  },
  "/operate/webhooks": {
    primaryAction: "Replay delivery",
    controls: () => statusControls("Search event, endpoint, response code..."),
    kpis: () => [kpi("Replay", "backed", "retry endpoint", "ready", "ok"), kpi("Payload", "drawer", "body preview", "linked", "running"), kpi("Provider", "Svix", "runtime metadata", "admin", "neutral"), kpi("Failures", "filter", "status", "fast", "warn")],
    rows: (snapshot) => snapshot.webhooks,
    tableTitle: "Webhook delivery inbox",
    tableDescription: "Outbound delivery attempts, response status, retry state, and payload previews.",
    secondaryTitle: "Subscriber context",
    secondary: (snapshot) => [
      card("Endpoints", "Configured outbound subscribers and signing state.", selectedRows(snapshot.webhookEndpoints, 5)),
      card("Payload drawer", "Rows open request headers, response body, and retry schedule."),
    ],
  },
  "/operate/notifications": {
    primaryAction: "Resend notification",
    controls: () => statusControls("Search recipient, template, channel, or status..."),
    kpis: () => [kpi("Sent", "live", "stats endpoint", "24h", "ok"), kpi("Failures", "live", "stats endpoint", "24h", "warn"), kpi("Channels", "env", "setup page owns config", "thin", "neutral"), kpi("Mute", "gap", "policy endpoint", "missing", "warn")],
    rows: (snapshot) => snapshot.notifications,
    tableTitle: "Notification delivery inbox",
    tableDescription: "Email, SMS, push, and log delivery audit with provider responses.",
    secondaryTitle: "Channel context",
    secondary: (snapshot) => [
      card("Delivery rows", "Delivery audit is distinct from Setup channel configuration.", selectedRows(snapshot.notifications, 5)),
      card("Missing action", "Mute future notifications requires a policy endpoint.", [{ label: "Gap", value: "mute policy endpoint", tone: "warn" }]),
    ],
  },
  "/operate/approvals": {
    primaryAction: "Create approval",
    controls: () => statusControls("Search approval id, kind, requester, tenant..."),
    kpis: () => [kpi("Pending", "live", "approval queue", "HITL", "running"), kpi("Decision", "drawer", "approve or deny", "audited", "ok"), kpi("Blocked run", "linked", "source object", "drill", "neutral"), kpi("Bulk", "guarded", "filtered decide", "careful", "warn")],
    rows: (snapshot) => snapshot.approvals,
    tableTitle: "Human decision queue",
    tableDescription: "Requests waiting on an operator before workflow execution continues.",
    secondaryTitle: "Decision context",
    secondary: (snapshot) => [
      card("Pending requests", "Rows link to related runs or jobs when present.", selectedRows(snapshot.approvals, 5)),
      card("Decision drawer", "Approve, deny, or cancel with note. Every decision produces audit context."),
    ],
  },
  "/operate/activity": {
    primaryAction: "Export CSV",
    controls: () => [select("Actor", "all", ["all", "user", "api key", "system", "anonymous"]), select("Resource", "all", ["all", "run", "tenant", "key", "budget"]), input("Search", "Search actor, verb, resource, IP...")],
    kpis: (snapshot) => pick(snapshot, [0, 1, 2, 4]),
    rows: (snapshot) => snapshot.activity,
    tableTitle: "Customer-side activity",
    tableDescription: "Actions customers took inside the customer app and what they triggered.",
    secondaryTitle: "Related impact",
    secondary: (snapshot) => [
      card("Triggered runs", "Activity rows drill into related runs, cost, and tenants.", selectedRows(snapshot.runs, 4)),
      card("Export", "CSV export can be client-side for filtered rows in v1."),
    ],
  },
  "/operate/health": {
    primaryAction: "Refresh checks",
    controls: () => setupControls("Search service, adapter, version, or route..."),
    kpis: () => [kpi("Runtime", "health", "/health", "live", "ok"), kpi("Ready", "ready", "/ready", "live", "ok"), kpi("Metrics", "summary", "route rollups", "live", "ok"), kpi("Deep stats", "missing", "PG/certs/workers", "gap", "warn")],
    rows: (snapshot) => [
      ...snapshot.services.map((service) => ({
        id: service.name,
        primary: service.name,
        secondary: `${service.adapter ?? "service"} · ${service.version}`,
        status: service.status,
        tone: service.status === "healthy" ? "ok" as StatusTone : service.status === "degraded" ? "warn" as StatusTone : "fail" as StatusTone,
        metric: service.adapter ?? "service",
        timestamp: service.checked,
      })),
      ...snapshot.observability,
    ],
    tableTitle: "Service topology",
    tableDescription: "Runtime and backing services with health checks surfaced through BackAI.",
    secondaryTitle: "Health caveats",
    secondary: (snapshot) => [
      card("Runtime metrics", "HTTP, heap, goroutine, and route rollups.", selectedRows(snapshot.observability, 5)),
      card("Missing deep checks", "DB stats, cert expiry, and worker internals need backend endpoints.", [{ label: "Gap", value: "deep checks endpoint", tone: "warn" }]),
    ],
  },
  "/operate/logs": {
    primaryAction: "Export JSONL",
    controls: () => [select("Level", "all", ["all", "debug", "info", "warn", "error"]), select("Service", "all", ["all", "runtime", "worker", "gateway"]), input("Search", "Search message, field, run id, tenant..."), switcher("Tail", "on")],
    kpis: () => [kpi("Rows", "bounded", "virtualized pattern", "scale", "ok"), kpi("Tail", "toggle", "pause/resume", "live", "running"), kpi("Fields", "expand", "structured JSON", "copy", "ok"), kpi("Export", "JSONL", "filtered rows", "ready", "neutral")],
    rows: (snapshot) => snapshot.logs,
    tableTitle: "Log stream",
    tableDescription: "Scale-safe log viewer with severity filters, tail mode, structured fields, and copy/export.",
    secondaryTitle: "Structured fields",
    secondary: (snapshot) => [
      card("Selected log", "Rows expose shareable timestamp filters and copyable JSON.", selectedRows(snapshot.logs, 4)),
      card("Tail mode", "Live tail shows connection status and pause/resume to avoid losing position."),
    ],
  },
  "/build/agents": {
    primaryAction: "Test agent",
    controls: () => [select("Status", "all", ["all", "healthy", "degraded"]), input("Search", "Search agent, reasoner, tag...")],
    kpis: (snapshot) => pick(snapshot, [5, 0, 2, 6]),
    rows: (snapshot) => snapshot.agents,
    tableTitle: "Agent registry",
    tableDescription: "Runtime agents, reasoners, versions, tags, and recent run context.",
    secondaryTitle: "Playground",
    secondary: (snapshot) => [
      card("Reasoners", "Agent detail includes schema and declared tools.", selectedRows(snapshot.reasoners, 5)),
      card("Invoke", "Generated form opens in a drawer and links resulting run back to Operate."),
    ],
  },
  "/build/reasoners": {
    primaryAction: "Open agent",
    controls: () => [select("Agent", "all", ["all", "support.triage", "coder.review"]), input("Search", "Search reasoner, schema, tool...")],
    kpis: () => [kpi("Listing", "derived", "from agents", "ok", "ok"), kpi("Analytics", "deferred", "cost/latency", "gap", "warn"), kpi("Schema", "preview", "input/output", "ready", "running"), kpi("Tools", "declared", "agent-level", "runtime", "ok")],
    rows: (snapshot) => snapshot.reasoners,
    tableTitle: "Reasoner inventory",
    tableDescription: "Cross-agent list of reasoning steps derived from the agent registry.",
    secondaryTitle: "Source links",
    secondary: (snapshot) => [
      card("Parent agents", "Every reasoner links back to its owning agent.", selectedRows(snapshot.agents, 5)),
      card("Deferred analytics", "Cost and latency stay on Cost until backend grouping is first-class.", [{ label: "Gap", value: "reasoner analytics endpoint", tone: "warn" }]),
    ],
  },
  "/build/tools": {
    primaryAction: "Invoke tool",
    controls: () => [tabs("Type", "all", ["all", "native", "adapter", "mcp"]), input("Search", "Search tool, adapter, schema...")],
    kpis: () => [kpi("Native", "live", "strict tools", "ready", "ok"), kpi("MCP", "live", "servers/tools", "ready", "ok"), kpi("Invoke", "drawer", "schema form", "ready", "running"), kpi("Usage", "deferred", "analytics", "gap", "warn")],
    rows: (snapshot) => snapshot.tools,
    tableTitle: "Tool inventory",
    tableDescription: "Native tools, adapter tools, and MCP tools with schemas and invoke entry points.",
    secondaryTitle: "Invoke context",
    secondary: (snapshot) => [
      card("Available tools", "Rows open a schema-generated invoke drawer.", selectedRows(snapshot.tools, 6)),
      card("Usage analytics", "Tool usage analytics are deferred in v1.", [{ label: "Gap", value: "tool usage endpoint", tone: "warn" }]),
    ],
  },
  "/build/skills": {
    primaryAction: "Install skill",
    controls: () => [select("Harness", "all", ["all", "claude-code", "codex", "gemini", "opencode"]), input("Search", "Search skill, MCP server, tool...")],
    kpis: () => [kpi("Servers", "live", "MCP", "ready", "ok"), kpi("Skills", "live", "registry", "ready", "ok"), kpi("Attach", "drawer", "agent binding", "audited", "running"), kpi("Status", "probe", "server reachability", "live", "ok")],
    rows: (snapshot) => snapshot.skills,
    tableTitle: "Skills and MCP servers",
    tableDescription: "Installed skills, MCP server status, exposed tools, and agent attachments.",
    secondaryTitle: "Server detail",
    secondary: (snapshot) => [card("Tool schemas", "MCP tool schemas are shown inline before invoke.", selectedRows(snapshot.skills, 6))],
  },
  "/build/harnesses": {
    primaryAction: "Probe harness",
    controls: () => [select("Provider", "all", ["all", "claude-code", "codex", "gemini", "opencode"]), input("Search", "Search provider, model, env...")],
    kpis: () => [kpi("Probe", "backed", "provider probe", "ready", "ok"), kpi("Auth", "visible", "required env", "clear", "running"), kpi("Logs", "drawer", "probe history", "linked", "neutral"), kpi("Disable", "deferred", "per agent", "gap", "warn")],
    rows: (snapshot) => snapshot.harnesses,
    tableTitle: "Harness registry",
    tableDescription: "Coding-agent harness binaries, versions, auth readiness, and probe status.",
    secondaryTitle: "Capability matrix",
    secondary: (snapshot) => [card("Providers", "Each harness row opens capability and probe detail.", selectedRows(snapshot.harnesses, 5))],
  },
  "/build/crons": {
    primaryAction: "Create cron",
    controls: () => [tabs("State", "all", ["all", "active", "paused"]), input("Search", "Search schedule, job, tenant...")],
    kpis: () => [kpi("Schedules", "backed", "crons endpoint", "ready", "ok"), kpi("Next run", "visible", "per cron", "ready", "running"), kpi("Pause", "backed", "active toggle", "audited", "ok"), kpi("Trigger now", "missing", "endpoint", "gap", "warn")],
    rows: (snapshot) => snapshot.crons,
    tableTitle: "Schedule board",
    tableDescription: "Cron jobs, target actions, next run, last run, and active state.",
    secondaryTitle: "Schedule detail",
    secondary: (snapshot) => [
      card("Next runs", "Cron rows use shareable query links for selected schedules.", selectedRows(snapshot.crons, 5)),
      card("Missing action", "Manual trigger is hidden until backend exposes an endpoint.", [{ label: "Gap", value: "trigger cron endpoint", tone: "warn" }]),
    ],
  },
  "/build/sandboxes": {
    primaryAction: "Run command",
    controls: () => [select("Image", "python:3.12", ["python:3.12", "node:22", "ubuntu:24.04"]), select("Network", "restricted", ["open", "restricted", "isolated"]), input("Command", "python -c 'print(42)'")],
    kpis: () => [kpi("Pool", "live", "warm/active/queued", "ready", "ok"), kpi("Run", "backed", "POST sandbox/run", "ready", "running"), kpi("Cancel", "backed", "DELETE run", "guarded", "ok"), kpi("Logs", "live", "tail route", "linked", "running")],
    rows: (snapshot) => snapshot.sandbox,
    tableTitle: "Sandbox workbench",
    tableDescription: "Run ad-hoc commands against the configured sandbox adapter and inspect pool pressure.",
    secondaryTitle: "Pool",
    secondary: (snapshot) => [card("Recent runs", "Completed commands drill into Operate Sandbox runs.", selectedRows(snapshot.sandbox, 5))],
  },
  "/build/modules": {
    primaryAction: "Open source",
    controls: () => [select("Status", "all", ["all", "enabled", "disabled"]), input("Search", "Search module, route, migration...")],
    kpis: () => [kpi("Modules", "backed", "runtime registry", "ready", "ok"), kpi("Writes", "code", "not console", "policy", "neutral"), kpi("Routes", "visible", "manifest", "ready", "running"), kpi("Migrations", "visible", "status", "thin", "warn")],
    rows: (snapshot) => snapshot.modules,
    tableTitle: "Workload modules",
    tableDescription: "Mounted modules, versions, routes, migrations, and source paths.",
    secondaryTitle: "Manifest",
    secondary: (snapshot) => [card("Installed modules", "Structural changes are code-owned in v1.", selectedRows(snapshot.modules, 6))],
  },
  "/build/data/tables": {
    primaryAction: "Copy table link",
    controls: () => [select("Schema", "all", ["all", "public", "suite", "agentfield"]), input("Search", "Search table or column...")],
    kpis: () => [kpi("Tables", "backed", "schema list", "ready", "ok"), kpi("Rows", "paged", "preview", "ready", "running"), kpi("RLS", "visible", "policy tabs", "ready", "ok"), kpi("Writes", "off", "read-only", "policy", "neutral")],
    rows: (snapshot) => snapshot.tables,
    tableTitle: "Database explorer",
    tableDescription: "Schema browser with selected table structure, policies, indexes, and row preview.",
    secondaryTitle: "Selected table",
    secondary: (snapshot) => [card("Tables", "Every table row should be shareable by schema and name.", selectedRows(snapshot.tables, 6))],
  },
  "/build/data/sql": {
    primaryAction: "Run query",
    controls: () => [select("Limit", "500", ["100", "500", "1000"]), switcher("Read only", "on"), input("Search", "Search saved snippets...")],
    kpis: () => [kpi("Read-only", "server", "enforced", "safe", "ok"), kpi("Results", "table", "bounded", "ready", "running"), kpi("History", "local", "until backend", "thin", "neutral"), kpi("Export", "CSV", "client", "ready", "ok")],
    rows: (snapshot) => snapshot.tables,
    tableTitle: "SQL workbench",
    tableDescription: "Read-only SQL editor with bounded results and copy/export affordances.",
    secondaryTitle: "Query context",
    secondary: () => [
      card("Starter query", "Use bounded read-only SQL for operational inspection.", undefined, "select * from suite_runs order by started_at desc limit 20;"),
      card("Saved snippets", "Persisted snippet/history storage can be added later."),
    ],
  },
  "/build/data/memory": {
    primaryAction: "Search memory",
    controls: () => [select("Scope", "tenant", ["global", "tenant", "agent", "session", "run"]), input("Search", "Semantic query or key prefix...")],
    kpis: () => [kpi("Entries", "backed", "memory list", "ready", "ok"), kpi("Search", "backed", "semantic", "ready", "running"), kpi("Embeddings", "visible", "per entry", "ready", "ok"), kpi("Delete", "guarded", "drawer", "audited", "warn")],
    rows: (snapshot) => snapshot.memory,
    tableTitle: "Memory explorer",
    tableDescription: "Per-scope memory entries, values, metadata, embedding state, and semantic search.",
    secondaryTitle: "Entry detail",
    secondary: (snapshot) => [card("Memory rows", "Entries link by scope and key.", selectedRows(snapshot.memory, 6))],
  },
  "/build/data/storage": {
    primaryAction: "Upload object",
    controls: () => [select("Bucket", "all", ["all", "tenant", "platform"]), input("Prefix", "tenant/acme/")],
    kpis: () => [kpi("Objects", "backed", "list route", "ready", "ok"), kpi("Signed URL", "backed", "preview", "ready", "running"), kpi("Upload", "backed", "storage/upload", "ready", "ok"), kpi("Delete", "guarded", "confirm", "audited", "warn")],
    rows: (snapshot) => snapshot.storage,
    tableTitle: "Object browser",
    tableDescription: "Storage keys, sizes, content types, signed URLs, and metadata.",
    secondaryTitle: "Object detail",
    secondary: (snapshot) => [card("Objects", "Rows open metadata and signed URL controls.", selectedRows(snapshot.storage, 6))],
  },
  "/build/data/search": {
    primaryAction: "Run search",
    controls: () => [select("Namespace", "all", ["all", "platform-docs", "tenant"]), input("Query", "Search indexed documents...")],
    kpis: () => [kpi("Query", "backed", "search route", "ready", "ok"), kpi("Upsert", "backed", "documents", "ready", "ok"), kpi("Stats", "missing", "index stats", "gap", "warn"), kpi("Latency", "result", "per query", "ready", "running")],
    rows: (snapshot) => snapshot.searchIndexes,
    tableTitle: "Search workbench",
    tableDescription: "Query indexes, inspect matches, and test document ingestion.",
    secondaryTitle: "Index context",
    secondary: (snapshot) => [
      card("Indexes", "Stats remain thin until backend exposes index rollups.", selectedRows(snapshot.searchIndexes, 5)),
      card("Gap", "Index statistics endpoint is missing.", [{ label: "Needed", value: "GET /api/v1/search/indexes", tone: "warn" }]),
    ],
  },
  "/build/feature-flags": {
    primaryAction: "Edit flag",
    controls: () => [tabs("State", "all", ["all", "enabled", "disabled"]), input("Search", "Search key, source, description...")],
    kpis: () => [kpi("Flags", "backed", "config endpoint", "ready", "ok"), kpi("Audit", "required", "mutation toast", "ready", "running"), kpi("Overrides", "deferred", "tenant history", "gap", "warn"), kpi("Rollout", "thin", "boolean first", "v1", "neutral")],
    rows: (snapshot) => snapshot.featureFlags,
    tableTitle: "Feature flags",
    tableDescription: "Runtime feature flags with drawer-based edits and audit references.",
    secondaryTitle: "Flag detail",
    secondary: (snapshot) => [card("Flags", "Rows open edit drawers with show-as-code.", selectedRows(snapshot.featureFlags, 6))],
  },
  "/build/api-explorer": {
    primaryAction: "Send request",
    controls: () => [select("Auth", "Operator session", ["Operator session", "Tenant key"]), input("Endpoint", "Search OpenAPI operation...")],
    kpis: () => [kpi("Schema", "backed", "/openapi.json", "ready", "ok"), kpi("Try it", "local", "same-origin", "ready", "running"), kpi("Downloads", "client", "json/yaml/types", "thin", "neutral"), kpi("Docs", "outside", "pure reference", "policy", "ok")],
    rows: () => [
      { id: "GET /openapi.json", primary: "OpenAPI schema", secondary: "Runtime schema source for this fork", status: "backed", tone: "ok", metric: "GET", timestamp: "current", href: "/openapi.json" },
      { id: "GET /api/v1/runs", primary: "List runs", secondary: "Execution stream endpoint", status: "backed", tone: "ok", metric: "GET", timestamp: "runtime" },
      { id: "POST /api/v1/llm/chat/completions", primary: "Chat completions", secondary: "Gateway-compatible LLM call", status: "backed", tone: "ok", metric: "POST", timestamp: "runtime" },
      { id: "POST /api/v1/sandbox/run", primary: "Sandbox run", secondary: "Run command in configured adapter", status: "backed", tone: "ok", metric: "POST", timestamp: "runtime" },
    ],
    tableTitle: "Runtime API explorer",
    tableDescription: "Try endpoints against this running fork with the selected auth context.",
    secondaryTitle: "Request context",
    secondary: () => [
      card("Auth selector", "Default uses operator session. Tenant mode uses a selected API key."),
      card("Copy as code", "Responses can be copied as curl, TypeScript, or Python snippets."),
    ],
  },
  "/customers/tenants": {
    primaryAction: "Add tenant",
    controls: () => [select("Plan", "all", ["all", "free", "team", "enterprise"]), input("Search", "Search tenant, slug, id...")],
    kpis: (snapshot) => pick(snapshot, [7, 2, 0, 1]),
    rows: (snapshot) => snapshot.tenants,
    tableTitle: "Tenant drilldown",
    tableDescription: "Customer workspaces, members, keys, spend, budget pressure, and status.",
    secondaryTitle: "Tenant detail",
    secondary: (snapshot) => [
      card("Budgets", "Budget status stays visible beside tenant operations.", snapshot.budgets.map((budget) => ({ label: budget.tenant, value: `${budget.used}%`, tone: budget.status }))),
      card("Shareable drilldown", "Tenant rows link by tenant id or slug."),
    ],
  },
  "/customers/api-keys": {
    primaryAction: "Issue key",
    controls: () => [select("Tenant", "all", ["all", "acme", "beta-labs"]), tabs("Status", "all", ["all", "active", "revoked", "expired"]), input("Search", "Search alias, prefix, tenant...")],
    kpis: () => [kpi("Issue", "backed", "one-time reveal", "ready", "ok"), kpi("Spend", "backed", "per key", "ready", "running"), kpi("Rotate", "revoke+issue", "until rotate endpoint", "gap", "warn"), kpi("Bulk", "guarded", "selected keys", "safe", "neutral")],
    rows: (snapshot) => snapshot.apiKeys,
    tableTitle: "API key operations",
    tableDescription: "Tenant keys, spend, limits, expiration, scopes, and revocation.",
    secondaryTitle: "Issue drawer",
    secondary: (snapshot) => [card("Keys", "New secrets reveal once and then become masked.", selectedRows(snapshot.apiKeys, 5))],
  },
  "/customers/members": {
    primaryAction: "Invite member",
    controls: () => [select("Role", "all", ["all", "owner", "admin", "member"]), input("Search", "Search email, tenant, role...")],
    kpis: () => [kpi("Users", "backed", "admin users", "ready", "ok"), kpi("Memberships", "backed", "roles", "ready", "ok"), kpi("Export", "backed", "GDPR", "ready", "ok"), kpi("Invite", "adapter", "auth dependent", "thin", "warn")],
    rows: (snapshot) => snapshot.members,
    tableTitle: "Members and access",
    tableDescription: "Users, tenant memberships, role assignments, export, and erase actions.",
    secondaryTitle: "Security detail",
    secondary: (snapshot) => [card("Sessions", "Session enumeration is capability-gated by auth adapter.", selectedRows(snapshot.sessions, 5))],
  },
  "/customers/sessions": {
    primaryAction: "Force logout",
    controls: () => [tabs("View", "active", ["active", "auth events"]), input("Search", "Search user, IP, user-agent, tenant...")],
    kpis: () => [kpi("Sessions", "degraded", "adapter capability", "caveat", "warn"), kpi("Auth events", "logs", "works across adapters", "ready", "running"), kpi("Logout", "hidden", "needs endpoint", "gap", "warn"), kpi("Suspicious", "derived", "log filters", "thin", "neutral")],
    rows: (snapshot) => snapshot.sessions,
    tableTitle: "Session security",
    tableDescription: "Active sessions when supported by the auth adapter, plus auth events from logs.",
    secondaryTitle: "Auth adapter caveat",
    secondary: (snapshot) => [
      card("Active sessions", "Better Auth can be read today; pluggable adapters may not enumerate sessions.", selectedRows(snapshot.sessions, 5)),
      card("Missing capability", "Universal session-listing capability is pending.", [{ label: "Needed", value: "auth supports session enumeration", tone: "warn" }]),
    ],
  },
  "/customers/budgets": {
    primaryAction: "Set budget",
    controls: () => [select("Status", "all", ["all", "ok", "near", "over"]), input("Search", "Search tenant or cap...")],
    kpis: (snapshot) => pick(snapshot, [2, 3, 7, 1]),
    rows: (snapshot) => snapshot.budgets.map((budget) => ({ id: budget.tenant, primary: budget.tenant, secondary: `${budget.cap} monthly cap`, status: budget.status, tone: budget.status, metric: `${budget.used}% used`, timestamp: "current" })),
    tableTitle: "Budget controls",
    tableDescription: "Tenant caps, alert thresholds, usage, and alert delivery status.",
    secondaryTitle: "Spend context",
    secondary: (snapshot) => [card("Top spenders", "Budget rows link into Cost with tenant scope.", selectedRows(snapshot.costRows, 5))],
  },
  "/customers/audit": {
    primaryAction: "Export audit",
    controls: () => [select("Actor", "all", ["all", "operator", "system", "api key"]), select("Action", "all", ["all", "create", "update", "delete"]), input("Search", "Search entity id or action...")],
    kpis: () => [kpi("Audit", "backed", "mutation feed", "ready", "ok"), kpi("Diff", "drawer", "JSON", "copy", "running"), kpi("Export", "client", "filtered CSV", "ready", "neutral"), kpi("Retention", "runtime", "policy", "visible", "ok")],
    rows: (snapshot) => snapshot.audit,
    tableTitle: "Audit timeline",
    tableDescription: "Full provenance feed for operator and system mutations.",
    secondaryTitle: "Diff detail",
    secondary: (snapshot) => [card("Recent mutations", "Rows open JSON diff and copyable resource links.", selectedRows(snapshot.audit, 6))],
  },
  "/customers/oauth": {
    primaryAction: "Authorize provider",
    controls: () => [select("Provider", "all", ["all", "google", "slack", "github"]), tabs("Status", "all", ["all", "active", "expired", "revoked", "failed"]), input("Search", "Search user, tenant, scope...")],
    kpis: () => [kpi("Connections", "backed", "OAuth list", "ready", "ok"), kpi("Providers", "backed", "provider list", "ready", "ok"), kpi("Refresh history", "missing", "endpoint", "gap", "warn"), kpi("Revoke", "backed", "provider delete", "guarded", "ok")],
    rows: (snapshot) => snapshot.oauth,
    tableTitle: "OAuth connections",
    tableDescription: "Tenant-scoped external grants used when agents act on behalf of users.",
    secondaryTitle: "Provider detail",
    secondary: (snapshot) => [card("Connections", "Rows are shareable by provider.", selectedRows(snapshot.oauth, 6))],
  },
  "/customers/billing": {
    primaryAction: "Open billing portal",
    controls: () => [select("Plan", "all", ["all", "free", "pro", "enterprise"]), input("Search", "Search tenant, plan, invoice...")],
    kpis: () => [kpi("Customers", "backed", "billing adapter", "ready", "ok"), kpi("Meters", "backed", "usage", "ready", "ok"), kpi("Portal", "backed", "provider link", "ready", "running"), kpi("Churn", "derived", "signals", "thin", "warn")],
    rows: (snapshot) => snapshot.billing,
    tableTitle: "Billing summary",
    tableDescription: "Per-tenant billing records, usage meters, plan state, and runtime-returned portal actions.",
    secondaryTitle: "Meter context",
    secondary: (snapshot) => [card("Billing rows", "Deep billing remains in the configured provider.", selectedRows(snapshot.billing, 6))],
  },
  "/setup/adapters": {
    primaryAction: "Review adapter contract",
    controls: () => setupControls("Search slot, adapter, capability..."),
    kpis: () => [kpi("Inventory", "backed", "admin adapters", "ready", "ok"), kpi("Tools", "live", "adapter rows", "ready", "ok"), kpi("Capabilities", "mixed", "slot accessors", "v1", "warn"), kpi("Swap", "env", "restart", "v1", "neutral")],
    rows: (snapshot) => snapshot.adapters.map((adapter) => ({ id: adapter.slot, primary: adapter.slot, secondary: adapter.description, status: adapter.adapter, tone: adapter.status, metric: "configured", timestamp: "current" })),
    tableTitle: "Adapter topology",
    tableDescription: "Every backend slot, current adapter, and capability caveat surfaced through BackAI.",
    secondaryTitle: "Capability contract",
    secondary: (snapshot) => [card("Runtime inventory", "BackAI owns the adapter slot contract; OSS services stay behind the runtime.", selectedAdapterRows(snapshot, 6))],
  },
  "/setup/auth-providers": {
    primaryAction: "Open auth docs",
    controls: () => setupControls("Search provider, origin, session config..."),
    kpis: () => [kpi("Current", "Better Auth", "config-backed", "ready", "ok"), kpi("Protocol", "landed", "docs", "moving", "running"), kpi("Capabilities", "pending", "runtime endpoint", "gap", "warn"), kpi("Secrets", "linked", "provider keys", "visible", "ok")],
    rows: (snapshot) => snapshot.sessions,
    tableTitle: "Auth provider inventory",
    tableDescription: "Configured auth providers, session behavior, trusted origins, and capability caveats.",
    secondaryTitle: "Adapter caveat",
    secondary: () => [card("Pending adapter shape", "Auth protocol is moving; runtime capability surface is implemented last.", [{ label: "Gap", value: "auth capabilities endpoint", tone: "warn" }])],
  },
  "/setup/llm-providers": {
    primaryAction: "Open gateway admin",
    controls: () => setupControls("Search model, provider, capability..."),
    kpis: () => [kpi("Models", "backed", "llm models", "ready", "ok"), kpi("Spend", "linked", "Cost", "ready", "running"), kpi("Gateway", "adapter", "moving", "caveat", "warn"), kpi("Fallback", "runtime", "gateway metadata", "thin", "neutral")],
    rows: (snapshot) => snapshot.llmProviders,
    tableTitle: "LLM gateway providers",
    tableDescription: "Models, provider cost, key readiness, and adapter metadata surfaced through BackAI.",
    secondaryTitle: "Gateway caveat",
    secondary: () => [card("Adapter work", "LLM gateway extraction is moving; this page is implemented after adapter docs settle.", [{ label: "Gap", value: "LLM gateway capabilities", tone: "warn" }])],
  },
  "/setup/sandbox": {
    primaryAction: "Open sandbox docs",
    controls: () => setupControls("Search pool, image, adapter..."),
    kpis: () => [kpi("Pool", "backed", "sandbox/pool", "ready", "ok"), kpi("Switching", "env", "restart", "v1", "neutral"), kpi("Runs", "linked", "Operate", "ready", "running"), kpi("Limits", "visible", "capability", "ready", "ok")],
    rows: (snapshot) => snapshot.sandbox,
    tableTitle: "Sandbox adapter",
    tableDescription: "Current sandbox adapter, pool status, capability limits, and run links.",
    secondaryTitle: "Pool context",
    secondary: (snapshot) => [card("Recent sandbox signals", "Pool and run rows link to Operate and Build surfaces.", selectedRows(snapshot.sandbox, 5))],
  },
  "/setup/webhook-subscribers": {
    primaryAction: "Add subscriber",
    controls: () => setupControls("Search endpoint, event, tenant..."),
    kpis: () => [kpi("Endpoints", "backed", "webhook endpoints", "ready", "ok"), kpi("Send test", "backed", "webhook send", "ready", "running"), kpi("Svix", "runtime metadata", "catalog", "admin", "neutral"), kpi("Signing", "visible", "secret ref", "ready", "ok")],
    rows: (snapshot) => snapshot.webhookEndpoints,
    tableTitle: "Webhook subscribers",
    tableDescription: "Outbound endpoint configuration, signing state, tenant scope, and test send.",
    secondaryTitle: "Delivery audit",
    secondary: (snapshot) => [card("Recent deliveries", "Delivery audit lives under Operate.", selectedRows(snapshot.webhooks, 5))],
  },
  "/setup/notifications": {
    primaryAction: "Send test",
    controls: () => setupControls("Search channel, template, adapter..."),
    kpis: () => [kpi("Channels", "env", "display only", "thin", "warn"), kpi("Test send", "backed", "notifications", "ready", "ok"), kpi("CRUD", "deferred", "endpoint", "gap", "warn"), kpi("Audit", "linked", "delivery page", "ready", "running")],
    rows: (snapshot) => snapshot.notifications,
    tableTitle: "Notification channels",
    tableDescription: "Channel configuration, key status, and test send. Delivery audit is separate.",
    secondaryTitle: "Config caveat",
    secondary: () => [card("Missing CRUD", "Full channel CRUD is deferred; v1 displays env config and sends tests.", [{ label: "Gap", value: "notification channel CRUD endpoint", tone: "warn" }])],
  },
  "/setup/secrets": {
    primaryAction: "Add secret",
    controls: () => setupControls("Search key, description, tenant..."),
    kpis: () => [kpi("Keys", "backed", "vault list", "ready", "ok"), kpi("Reveal", "audited", "once", "guarded", "warn"), kpi("Rotate", "backed", "rotate route", "ready", "ok"), kpi("Delete", "dialog", "destructive", "safe", "warn")],
    rows: (snapshot) => snapshot.secrets,
    tableTitle: "Secrets vault",
    tableDescription: "Secret names, rotation windows, usage references, and guarded reveal controls.",
    secondaryTitle: "Usage context",
    secondary: (snapshot) => [card("Secrets", "Values are never shown in lists. Reveal is audited.", selectedRows(snapshot.secrets, 6))],
  },
  "/setup/observability": {
    primaryAction: "Open metrics",
    controls: () => setupControls("Search metric, exporter, route..."),
    kpis: () => [kpi("Metrics", "backed", "summary", "ready", "ok"), kpi("Traces", "missing", "backend query", "thin", "warn"), kpi("Logs", "linked", "Operate", "ready", "ok"), kpi("Config", "env", "no runtime PUT", "v1", "neutral")],
    rows: (snapshot) => snapshot.observability,
    tableTitle: "Observability setup",
    tableDescription: "Metrics, log, trace, and exporter status through runtime-owned contracts.",
    secondaryTitle: "Runtime metrics",
    secondary: (snapshot) => [card("Metric rollups", "Runtime summary powers Health and Observability.", selectedRows(snapshot.observability, 6))],
  },
  "/setup/billing-adapter": {
    primaryAction: "Open billing admin",
    controls: () => setupControls("Search plan, adapter, meter..."),
    kpis: () => [kpi("Adapter", "env", "Stripe/Lago", "v1", "neutral"), kpi("Customers", "backed", "billing list", "ready", "ok"), kpi("Meters", "backed", "usage", "ready", "ok"), kpi("Switch", "env", "restart", "v1", "neutral")],
    rows: (snapshot) => snapshot.billing,
    tableTitle: "Billing adapter",
    tableDescription: "Provider selection, API key status, plan mapping, customers, and meters.",
    secondaryTitle: "Provider context",
    secondary: (snapshot) => [card("Billing records", "Deep billing remains in Stripe or Lago.", selectedRows(snapshot.billing, 6))],
  },
  "/setup/deploy-targets": {
    primaryAction: "Open provider",
    controls: () => setupControls("Search target, provider, status..."),
    kpis: () => [kpi("Provisioning", "missing", "no runtime endpoint", "gap", "fail"), kpi("Compose", "informational", "local", "ready", "neutral"), kpi("Provider", "missing", "runtime metadata", "manual", "neutral"), kpi("Secrets", "linked", "setup", "ready", "ok")],
    rows: (snapshot) => snapshot.deployTargets,
    tableTitle: "Deploy targets",
    tableDescription: "Informational view of deployment targets. No in-product provisioning in v1.",
    secondaryTitle: "Missing endpoint",
    secondary: () => [card("No provisioning API", "Deploy target status is informational until the runtime owns deploy status.", [{ label: "Gap", value: "deploy status endpoint", tone: "warn" }])],
  },
  "/brand": {
    primaryAction: "Copy file path",
    controls: () => [input("Search", "Search brand token, color, asset...")],
    kpis: () => [kpi("Source", "brand.yaml", "file-backed", "v1", "neutral"), kpi("Endpoint", "missing", "admin brand", "gap", "warn"), kpi("Editing", "code", "file edit", "policy", "neutral"), kpi("Reload", "thin", "if hot reload", "manual", "neutral")],
    rows: (snapshot) => [
      { id: "brand-name", primary: "Brand name", secondary: "Displayed public name from brand.yaml", status: "file", tone: "neutral", metric: "BackAI", timestamp: "current" },
      { id: "primary", primary: "Primary color", secondary: "Monochrome admin theme remains separate", status: "file", tone: "neutral", metric: "#fafafa", timestamp: "current" },
      { id: "logo", primary: "Logo", secondary: "Asset preview when configured", status: "file", tone: "neutral", metric: "logo", timestamp: "current" },
      ...snapshot.adapters.slice(0, 3).map((adapter) => ({ id: `brand-adapter:${adapter.slot}`, primary: adapter.slot, secondary: adapter.description, status: adapter.adapter, tone: adapter.status, metric: "adapter", timestamp: "current" })),
    ],
    tableTitle: "Brand file display",
    tableDescription: "Read-only display of operator-owned brand.yaml. Editing is a file edit in v1.",
    secondaryTitle: "File contract",
    secondary: () => [
      card("Endpoint gap", "The admin reads file-level brand information until a brand endpoint exists.", [{ label: "Needed", value: "GET /api/v1/admin/brand", tone: "warn" }]),
      card("Path", "Copy this path into the editor.", undefined, "brand.yaml"),
    ],
  },
}

export function buildPageModel(pathname: string, snapshot: OperatorSnapshot): PageModel | null {
  const path = normalizePath(pathname)
  const navItem = navItemForPath(path)
  if (!allNavItems.some((item) => item.href === navItem.href) || navItem.href !== path) {
    return null
  }
  const definition = definitions[path]
  if (!definition) return null
  const rows = definition.rows(snapshot)
  const modelBase = {
    dataTruth: navItem.dataTruth,
    apiGap: navItem.apiGap,
    adapter: navItem.adapter,
  }

  return {
    path,
    group: groupForPath(path),
    title: navItem.title,
    description: navItem.description,
    live: Boolean(navItem.live && snapshot.source === "live"),
    source: snapshot.source,
    generatedAt: snapshot.generatedAt,
    adapter: navItem.adapter,
    dataTruth: navItem.dataTruth,
    apiGap: navItem.apiGap,
    archetype: navItem.archetype,
    primaryAction: definition.primaryAction,
    controls: definition.controls(snapshot),
    kpis: definition.kpis(snapshot),
    tableTitle: definition.tableTitle,
    tableDescription: definition.tableDescription,
    tableColumns: definition.columns ?? defaultColumns,
    rows,
    secondaryTitle: definition.secondaryTitle,
    secondary: [statusCard(modelBase, snapshot), ...definition.secondary(snapshot)],
  }
}
