// SPDX-License-Identifier: Apache-2.0

import type { ConsoleRow, Kpi, OperatorSnapshot, StatusTone } from "@/lib/new-admin/data"
import { allNavItems, groupForPath, navItemForPath, normalizePath } from "@/lib/new-admin/navigation"

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
  generatedAt: string
  adapter?: string
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
  adapter?: string
  primaryAction: string
  controls?: PageControl[]
  kpis?: (snapshot: OperatorSnapshot) => Kpi[]
  tableTitle: string
  tableDescription: string
  tableColumns?: [string, string, string, string]
  rows: (snapshot: OperatorSnapshot) => ConsoleRow[]
  secondaryTitle: string
  secondary: (snapshot: OperatorSnapshot) => PageCard[]
}

const defaultColumns: [string, string, string, string] = ["Object", "Context", "Metric", "Updated"]

const platformControls: PageControl[] = [
  { kind: "select", label: "Scope", value: "Platform", options: ["Platform", "acme", "beta-labs", "delta-health"] },
  { kind: "select", label: "Range", value: "24h", options: ["1h", "24h", "7d", "30d"] },
  { kind: "input", label: "Search", placeholder: "Filter current page..." },
]

const developControls: PageControl[] = [
  { kind: "select", label: "Runtime", value: "local", options: ["local", "staging", "production"] },
  { kind: "select", label: "Tenant", value: "Platform", options: ["Platform", "acme", "beta-labs", "delta-health"] },
  { kind: "input", label: "Search", placeholder: "Find endpoint, snippet, or recipe..." },
]

const operateControls: PageControl[] = [
  { kind: "select", label: "Tenant", value: "Platform", options: ["Platform", "acme", "beta-labs", "delta-health"] },
  { kind: "select", label: "Range", value: "24h", options: ["15m", "1h", "24h", "7d"] },
  { kind: "tabs", label: "State", value: "all", options: ["all", "live", "failed", "queued"] },
]

const buildControls: PageControl[] = [
  { kind: "select", label: "Source", value: "Runtime", options: ["Runtime", "Database", "Config", "Repo"] },
  { kind: "select", label: "Tenant", value: "Platform", options: ["Platform", "acme", "beta-labs", "delta-health"] },
  { kind: "input", label: "Search", placeholder: "Find agent, table, module, or flag..." },
]

const customerControls: PageControl[] = [
  { kind: "select", label: "Tenant", value: "Platform", options: ["Platform", "acme", "beta-labs", "delta-health"] },
  { kind: "select", label: "Plan", value: "All plans", options: ["All plans", "Free", "Team", "Enterprise"] },
  { kind: "input", label: "Search", placeholder: "Find customer, key, member, or audit entry..." },
]

const setupControls: PageControl[] = [
  { kind: "select", label: "Environment", value: "local", options: ["local", "staging", "production"] },
  { kind: "tabs", label: "Readiness", value: "all", options: ["all", "ready", "warning", "missing"] },
  { kind: "switch", label: "Show secrets", value: "off" },
]

function makeKpi(label: string, value: string, detail: string, trend: string, tone: StatusTone = "neutral", sparkline = [8, 10, 9, 12, 14, 13, 16, 15]): Kpi {
  return { label, value, detail, trend, tone, sparkline }
}

function pickKpis(snapshot: OperatorSnapshot, indexes: number[]) {
  return indexes.map((index) => snapshot.kpis[index]).filter(Boolean).slice(0, 4)
}

function storageTotal(rows: ConsoleRow[]) {
  if (rows.length === 0) return "0 B"
  if (rows.length === 1) return rows[0]?.metric ?? "1 object"
  return `${rows.length} objects`
}

function row(
  id: string,
  primary: string,
  secondary: string,
  status: string,
  tone: StatusTone,
  metric: string,
  timestamp: string
): ConsoleRow {
  return { id, primary, secondary, status, tone, metric, timestamp }
}

function card(title: string, description: string, rows?: PageCard["rows"], code?: string): PageCard {
  return { title, description, rows, code }
}

function serviceRows(snapshot: OperatorSnapshot) {
  return snapshot.services.map((service) =>
    row(
      service.name,
      service.name,
      `${service.adapter ?? "adapter"} · ${service.version}`,
      service.status,
      service.status === "healthy" ? "ok" : service.status === "degraded" ? "warn" : "fail",
      service.adapter ?? "service",
      service.checked
    )
  )
}

function budgetRows(snapshot: OperatorSnapshot) {
  return snapshot.budgets.map((budget) =>
    row(
      budget.tenant,
      budget.tenant,
      `${budget.cap} monthly cap`,
      budget.status === "ok" ? "within cap" : budget.status === "warn" ? "near cap" : "over cap",
      budget.status,
      `${budget.used}% used`,
      "current period"
    )
  )
}

function adapterRows(snapshot: OperatorSnapshot) {
  return snapshot.adapters.map((adapter) =>
    row(adapter.slot, adapter.slot, adapter.description, adapter.adapter, adapter.status, "configured", "current")
  )
}

function quickstartSecondary(snapshot: OperatorSnapshot): PageCard[] {
  return [
    card("Runtime", "The values developers need before making a first call.", [
      { label: "Base URL", value: snapshot.snippets.runtimeUrl },
      { label: "Tenant key", value: snapshot.snippets.tenantKey },
      { label: "Runtime", value: snapshot.runtimeStatus, tone: snapshot.source === "live" ? "ok" : "warn" },
    ]),
    card("First call", "Copy-safe starter request scoped to the selected tenant.", undefined, snapshot.snippets.curl),
  ]
}

const definitions: Record<string, PageDefinition> = {
  "/": {
    adapter: "BackAI runtime",
    primaryAction: "Open command center",
    controls: platformControls,
    kpis: (snapshot) => pickKpis(snapshot, [0, 1, 2, 4]),
    tableTitle: "Platform activity",
    tableDescription: "The most recent operator-relevant events across requests, runs, alerts, tenants, and budgets.",
    rows: (snapshot) => snapshot.activity,
    secondaryTitle: "System posture",
    secondary: (snapshot) => [
      card("Services", "Live service health and configured adapters.", serviceRows(snapshot).slice(0, 5).map((service) => ({
        label: service.primary,
        value: `${service.status} · ${service.metric}`,
        tone: service.tone,
      }))),
      card("Budget pressure", "Tenant spend pressure for the current billing window.", snapshot.budgets.map((budget) => ({
        label: budget.tenant,
        value: `${budget.used}%`,
        tone: budget.status,
      }))),
    ],
  },
  "/develop/quickstart": {
    adapter: "OpenAPI + SDK",
    primaryAction: "Issue developer key",
    controls: developControls,
    kpis: () => [
      makeKpi("First call", "3 min", "curl, TS, Python", "ready", "ok"),
      makeKpi("Auth context", "1 key", "selected tenant", "masked", "running"),
      makeKpi("SDKs", "3", "typescript, python, go", "current", "ok"),
      makeKpi("Examples", "5", "common runtime paths", "+2", "neutral"),
    ],
    tableTitle: "Starter checklist",
    tableDescription: "Everything a developer needs to connect to this BackAI fork without leaving the console.",
    rows: (snapshot) => [
      row("runtime-url", "Runtime URL", "Active backend endpoint for SDK and curl calls.", "ready", "ok", snapshot.snippets.runtimeUrl, "current"),
      row("tenant-key", "Tenant API key", "Scoped bearer key for the selected tenant.", "masked", "running", snapshot.snippets.tenantKey, "current"),
      row("install-ts", "Install TypeScript SDK", "npm, pnpm, or bun package command.", "ready", "ok", "1 command", "copy"),
      row("install-py", "Install Python SDK", "pip or uv package command.", "ready", "ok", "1 command", "copy"),
      row("first-run", "Create first run", "Call an agent and inspect request, run, and trace IDs.", "ready", "ok", "POST /agents", "guided"),
    ],
    secondaryTitle: "Copy panel",
    secondary: quickstartSecondary,
  },
  "/develop/api-explorer": {
    adapter: "OpenAPI explorer",
    primaryAction: "Send request",
    controls: developControls,
    kpis: () => [
      makeKpi("Endpoint groups", "9", "agents, runs, data, admin", "mapped", "ok"),
      makeKpi("Auth mode", "Bearer", "tenant key", "active", "running"),
      makeKpi("Schema", "3.1", "OpenAPI", "valid", "ok"),
      makeKpi("Last response", "202", "run accepted", "268 ms", "ok"),
    ],
    tableTitle: "Endpoint surface",
    tableDescription: "Testable runtime and admin endpoints with the current auth context.",
    rows: () => [
      row("POST /api/v1/agents/{id}/runs", "Create agent run", "Body schema, streaming toggle, and tenant header.", "ready", "ok", "POST", "runtime"),
      row("GET /api/v1/runs", "List runs", "Filters by tenant, status, agent, and time window.", "ready", "ok", "GET", "runtime"),
      row("GET /api/v1/cost", "Cost summary", "Provider spend, cache savings, forecasts, and budgets.", "ready", "ok", "GET", "operate"),
      row("POST /api/admin/keys", "Issue API key", "Tenant-scoped key creation with limit fields.", "guarded", "warn", "POST", "admin"),
      row("GET /api/db/tables", "Database tables", "Schema inventory and RLS posture.", "ready", "ok", "GET", "build"),
    ],
    secondaryTitle: "Request context",
    secondary: (snapshot) => [
      card("Headers", "Headers applied to test requests.", [
        { label: "Authorization", value: "Bearer tenant key", tone: "running" },
        { label: "X-BackAI-Tenant", value: "selected scope" },
        { label: "Runtime", value: snapshot.snippets.runtimeUrl },
      ]),
      card("Response shape", "IDs returned by successful requests are linkable back into Runs and Traces.", [
        { label: "run_id", value: "opens Runs" },
        { label: "trace_id", value: "opens Traces" },
        { label: "cost_event_id", value: "opens Cost" },
      ]),
    ],
  },
  "/develop/sdk-cli": {
    adapter: "SDK packages",
    primaryAction: "Copy install",
    controls: developControls,
    kpis: () => [
      makeKpi("SDKs", "3", "typescript, python, go", "stable", "ok"),
      makeKpi("CLI", "1", "backai command", "local", "running"),
      makeKpi("Examples", "12", "runtime and admin", "+4", "neutral"),
      makeKpi("Typed calls", "100%", "from schema", "generated", "ok"),
    ],
    tableTitle: "SDK and CLI commands",
    tableDescription: "Install, configure, run, and inspect the system from code or terminal.",
    rows: (snapshot) => [
      row("typescript", "TypeScript client", "Runtime calls, admin operations, and typed responses.", "ready", "ok", "npm", "current"),
      row("python", "Python client", "Async-first calls for worker and notebook use.", "ready", "ok", "pip", "current"),
      row("go", "Go client", "Server integrations with context-aware requests.", "ready", "ok", "go", "current"),
      row("cli-config", "backai login", "Stores runtime URL and tenant key locally.", "ready", "ok", "CLI", "local"),
      row("cli-runs", "backai runs tail", "Streams live run and error events.", "ready", "ok", "CLI", "live"),
      row("snippet", "List runs", "Canonical runtime list call.", "ready", "ok", "code", snapshot.generatedAt.slice(11, 19)),
    ],
    secondaryTitle: "Snippets",
    secondary: (snapshot) => [
      card("TypeScript", "Common runtime list call.", undefined, snapshot.snippets.typescript),
      card("Python", "Equivalent async-friendly client call.", undefined, snapshot.snippets.python),
      card("Go", "Server-side context-aware call.", undefined, snapshot.snippets.go),
    ],
  },
  "/develop/schema": {
    adapter: "OpenAPI",
    primaryAction: "Download schema",
    controls: developControls,
    kpis: () => [
      makeKpi("Spec", "3.1", "OpenAPI", "valid", "ok"),
      makeKpi("Schemas", "46", "request/response", "+3", "neutral"),
      makeKpi("Clients", "3", "generated", "current", "ok"),
      makeKpi("Breaking changes", "0", "last build", "clean", "ok"),
    ],
    tableTitle: "Schema artifacts",
    tableDescription: "Published contract files and generated client targets.",
    rows: () => [
      row("openapi-json", "openapi.json", "Machine-readable canonical API contract.", "valid", "ok", "842 KiB", "generated"),
      row("openapi-yaml", "openapi.yaml", "Human-readable contract for review and docs.", "valid", "ok", "916 KiB", "generated"),
      row("typescript-types", "TypeScript types", "Generated from OpenAPI and exported by SDK.", "current", "ok", "136 types", "generated"),
      row("python-models", "Python models", "Pydantic-compatible request and response models.", "current", "ok", "121 models", "generated"),
      row("contract-diff", "Contract diff", "Last schema diff against main branch.", "clean", "ok", "0 breaking", "CI"),
    ],
    secondaryTitle: "Generation",
    secondary: () => [
      card("Client targets", "Supported generated clients for product and customer integrations.", [
        { label: "TypeScript", value: "runtime + admin", tone: "ok" },
        { label: "Python", value: "runtime + data", tone: "ok" },
        { label: "Go", value: "runtime", tone: "running" },
      ]),
      card("Validation gates", "Checks that should block backend drift before release.", [
        { label: "Schema diff", value: "required", tone: "ok" },
        { label: "Example compile", value: "required", tone: "ok" },
        { label: "E2E smoke", value: "recommended", tone: "running" },
      ]),
    ],
  },
  "/develop/recipes": {
    adapter: "Integration guide",
    primaryAction: "Create recipe",
    controls: developControls,
    kpis: () => [
      makeKpi("Recipes", "8", "copy-ready", "+2", "neutral"),
      makeKpi("Webhook flows", "3", "in/out/replay", "ready", "ok"),
      makeKpi("Job flows", "2", "queue and cron", "ready", "ok"),
      makeKpi("Billing flows", "2", "meter and portal", "draft", "running"),
    ],
    tableTitle: "Integration recipes",
    tableDescription: "Opinionated glue patterns for common product and customer workflows.",
    rows: () => [
      row("recipe-agent-call", "Call an agent from an app", "Server action with tenant key, run link, and trace link.", "ready", "ok", "15 min", "runtime"),
      row("recipe-webhook-in", "Receive customer webhook", "Verify signature, enqueue job, create audit row.", "ready", "ok", "25 min", "webhooks"),
      row("recipe-webhook-out", "Notify customer endpoint", "Emit signed event and replay failed delivery.", "ready", "ok", "20 min", "webhooks"),
      row("recipe-metering", "Meter LLM usage", "Record spend and sync to billing adapter.", "draft", "running", "30 min", "billing"),
      row("recipe-rag", "Attach search to agent", "Index docs, query search, write memory.", "ready", "ok", "40 min", "build"),
      row("recipe-queue", "Run async workflow", "Enqueue, retry, inspect job and trace.", "ready", "ok", "20 min", "queue"),
    ],
    secondaryTitle: "Recipe policy",
    secondary: () => [
      card("Reusable shape", "Each recipe should include route, auth, data touchpoints, trace links, and rollback notes.", [
        { label: "Auth", value: "tenant scoped", tone: "ok" },
        { label: "Observability", value: "trace linked", tone: "ok" },
        { label: "Idempotency", value: "explicit", tone: "running" },
      ]),
    ],
  },
  "/operate/runs": {
    adapter: "AgentField runtime",
    primaryAction: "Replay run",
    controls: operateControls,
    kpis: (snapshot) => pickKpis(snapshot, [0, 5, 6, 2]),
    tableTitle: "Run stream",
    tableDescription: "Live executions with tenant, model, cost, duration, and debug entry points.",
    rows: (snapshot) => snapshot.runs,
    secondaryTitle: "Debug lens",
    secondary: (snapshot) => [
      card("Selected run", "The first row is preselected for keyboard-friendly triage.", [
        { label: "Status", value: snapshot.runs[0]?.status ?? "none", tone: snapshot.runs[0]?.tone ?? "neutral" },
        { label: "Cost and duration", value: snapshot.runs[0]?.metric ?? "n/a" },
        { label: "Trace", value: "linked in Traces" },
      ]),
      card("Run actions", "Operator actions stay in drawers so the list remains dense.", [
        { label: "Replay", value: "available", tone: "running" },
        { label: "Cancel", value: "guarded", tone: "warn" },
        { label: "Export", value: "json" },
      ]),
    ],
  },
  "/operate/cost": {
    adapter: "LiteLLM + billing",
    primaryAction: "Set budget",
    controls: operateControls,
    kpis: (snapshot) => pickKpis(snapshot, [2, 3, 7, 0]),
    tableTitle: "Cost ledger",
    tableDescription: "Tenant and agent spend with budget pressure, provider context, and cache impact.",
    rows: (snapshot) => snapshot.costRows,
    secondaryTitle: "Budget controls",
    secondary: (snapshot) => [
      card("Budget pressure", "Current period usage by tenant.", snapshot.budgets.map((budget) => ({
        label: budget.tenant,
        value: `${budget.used}%`,
        tone: budget.status,
      }))),
      card("Savings", "Cache and model-routing opportunities surfaced for operators.", [
        { label: "Cache savings", value: "$18.42", tone: "ok" },
        { label: "Top model", value: "claude-sonnet" },
        { label: "Forecast", value: "$2.8k month", tone: "warn" },
      ]),
    ],
  },
  "/operate/errors": {
    adapter: "Runtime alerts",
    primaryAction: "Acknowledge error",
    controls: operateControls,
    kpis: (snapshot) => pickKpis(snapshot, [1, 6, 4, 0]),
    tableTitle: "Failure triage",
    tableDescription: "Grouped errors across agent runs, handlers, queues, providers, and webhooks.",
    rows: (snapshot) => snapshot.errors,
    secondaryTitle: "Triage state",
    secondary: () => [
      card("Ownership", "Every active error should have a route to run, trace, tenant, and owning adapter.", [
        { label: "Grouping", value: "fingerprint", tone: "ok" },
        { label: "Mute", value: "time-boxed", tone: "running" },
        { label: "Escalation", value: "Slack or email", tone: "neutral" },
      ]),
      card("Severity guide", "Failure color is reserved for broken user-visible work.", [
        { label: "Fail", value: "red", tone: "fail" },
        { label: "Warn", value: "amber", tone: "warn" },
        { label: "Running", value: "neutral", tone: "running" },
      ]),
    ],
  },
  "/operate/traces": {
    adapter: "OpenTelemetry",
    primaryAction: "Open trace",
    controls: operateControls,
    kpis: () => [
      makeKpi("p95 latency", "2.8 s", "agent calls", "+0.2 s", "warn"),
      makeKpi("Span errors", "7", "last 24h", "-3", "ok"),
      makeKpi("Slowest span", "llm.chat", "provider wait", "1.9 s", "running"),
      makeKpi("Sampling", "100%", "local runtime", "full", "ok"),
    ],
    tableTitle: "Trace list",
    tableDescription: "Request spans, critical path timing, and linked run context.",
    rows: (snapshot) => snapshot.traces,
    secondaryTitle: "Span tree",
    secondary: () => [
      card("Critical path", "Collapsed span tree for the selected request.", [
        { label: "HTTP handler", value: "42 ms", tone: "ok" },
        { label: "Agent graph", value: "520 ms", tone: "running" },
        { label: "LLM call", value: "1.1 s", tone: "warn" },
        { label: "Persist event", value: "18 ms", tone: "ok" },
      ]),
    ],
  },
  "/operate/queue": {
    adapter: "River jobs",
    primaryAction: "Drain queue",
    controls: operateControls,
    kpis: (snapshot) => pickKpis(snapshot, [4, 5, 6, 1]),
    tableTitle: "Async jobs",
    tableDescription: "Queue depth, retry pressure, scheduled jobs, and dead-letter candidates.",
    rows: (snapshot) => snapshot.queue,
    secondaryTitle: "Worker posture",
    secondary: () => [
      card("Workers", "Capacity and lag for the selected environment.", [
        { label: "Concurrency", value: "12 slots", tone: "ok" },
        { label: "Oldest queued", value: "42 s", tone: "running" },
        { label: "Dead letters", value: "0", tone: "ok" },
      ]),
      card("Retry policy", "Default job behavior applied unless a module overrides it.", [
        { label: "Max attempts", value: "5" },
        { label: "Backoff", value: "exponential" },
        { label: "Cron", value: "enabled", tone: "ok" },
      ]),
    ],
  },
  "/operate/webhooks": {
    adapter: "Svix",
    primaryAction: "Replay delivery",
    controls: operateControls,
    kpis: () => [
      makeKpi("Deliveries", "1,842", "24h", "+8%", "ok"),
      makeKpi("Failed", "2", "needs replay", "-1", "warn"),
      makeKpi("Median latency", "244 ms", "outbound", "stable", "ok"),
      makeKpi("Subscribers", "7", "active endpoints", "+1", "neutral"),
    ],
    tableTitle: "Webhook deliveries",
    tableDescription: "Inbound and outbound events with response status, signing, and replay state.",
    rows: (snapshot) => snapshot.webhooks,
    secondaryTitle: "Subscriber health",
    secondary: () => [
      card("Replay rules", "Failed deliveries stay actionable without leaving the page.", [
        { label: "Retry window", value: "72h", tone: "running" },
        { label: "Signature", value: "required", tone: "ok" },
        { label: "Event catalog", value: "12 events" },
      ]),
    ],
  },
  "/operate/cache": {
    adapter: "LLM cache",
    primaryAction: "Purge namespace",
    controls: operateControls,
    kpis: () => [
      makeKpi("Hit rate", "31%", "last 24h", "+4 pts", "ok"),
      makeKpi("Saved cost", "$18.42", "last 24h", "+11%", "ok"),
      makeKpi("Saved latency", "4.8 h", "aggregate", "stable", "running"),
      makeKpi("Namespaces", "6", "tenant scoped", "clean", "neutral"),
    ],
    tableTitle: "Cache namespaces",
    tableDescription: "Reusable prompt and response caches with hit rate, cost savings, and invalidation controls.",
    rows: (snapshot) => snapshot.costRows,
    secondaryTitle: "Invalidation",
    secondary: () => [
      card("Purge guardrails", "Purge is scoped and previewed before execution.", [
        { label: "Tenant prefix", value: "required", tone: "ok" },
        { label: "Dry run", value: "default", tone: "running" },
        { label: "Audit row", value: "created", tone: "ok" },
      ]),
    ],
  },
  "/build/agents": {
    adapter: "AgentField registry",
    primaryAction: "Test agent",
    controls: buildControls,
    kpis: (snapshot) => [
      makeKpi("Agents", String(snapshot.agents.length), "registered", "runtime", "ok"),
      makeKpi("Reasoners", "7", "across agents", "+1", "neutral"),
      makeKpi("Healthy", String(snapshot.agents.filter((agent) => agent.tone === "ok").length), "runtime check", "current", "ok"),
      makeKpi("Drafts", "2", "not deployed", "repo", "running"),
    ],
    tableTitle: "Agent registry",
    tableDescription: "Agents, reasoner graphs, versions, runtime health, and direct playground links.",
    rows: (snapshot) => snapshot.agents,
    secondaryTitle: "Agent operations",
    secondary: () => [
      card("Expected detail pages", "Each agent gets a detail route and a playground route.", [
        { label: "Detail", value: "/build/agents/{id}" },
        { label: "Playground", value: "/build/agents/{id}/playground" },
        { label: "Version source", value: "runtime manifest", tone: "running" },
      ]),
      card("Deployment gates", "Before an agent is exposed to customers, these fields should be present.", [
        { label: "Input schema", value: "required", tone: "ok" },
        { label: "Cost policy", value: "required", tone: "ok" },
        { label: "Replay sample", value: "recommended", tone: "running" },
      ]),
    ],
  },
  "/build/data/tables": {
    adapter: "Postgres",
    primaryAction: "Open table",
    controls: buildControls,
    kpis: () => [
      makeKpi("Tables", "18", "tenant aware", "current", "ok"),
      makeKpi("RLS coverage", "100%", "customer tables", "clean", "ok"),
      makeKpi("Rows", "10.7k", "estimated", "+2%", "neutral"),
      makeKpi("Writes", "184", "last hour", "normal", "running"),
    ],
    tableTitle: "Database tables",
    tableDescription: "Schema inventory with estimated rows, storage size, and isolation posture.",
    rows: (snapshot) => snapshot.tables,
    secondaryTitle: "Table policy",
    secondary: () => [
      card("Isolation posture", "Customer-facing tables must expose tenant boundaries clearly.", [
        { label: "Tenant column", value: "required", tone: "ok" },
        { label: "RLS", value: "on", tone: "ok" },
        { label: "Audit mutations", value: "on", tone: "ok" },
      ]),
    ],
  },
  "/build/data/sql": {
    adapter: "Postgres",
    primaryAction: "Run read-only query",
    controls: buildControls,
    kpis: () => [
      makeKpi("Saved queries", "9", "operator vetted", "+1", "neutral"),
      makeKpi("Read-only", "on", "enforced", "safe", "ok"),
      makeKpi("p95 query", "74 ms", "last 24h", "-8 ms", "ok"),
      makeKpi("Exports", "3", "today", "csv", "running"),
    ],
    tableTitle: "SQL workbench",
    tableDescription: "Read-only saved operational queries and recent safe query runs.",
    rows: () => [
      row("sql-cost-by-tenant", "Cost by tenant", "Aggregates cost_events over selected range.", "saved", "ok", "31 ms", "today"),
      row("sql-failed-runs", "Failed runs", "Lists failures with tenant, agent, and trace IDs.", "saved", "ok", "44 ms", "today"),
      row("sql-key-spend", "API key spend", "Maps key prefix to cost and request count.", "saved", "ok", "52 ms", "today"),
      row("sql-queue-lag", "Queue lag", "Checks oldest waiting job by queue.", "saved", "ok", "18 ms", "today"),
    ],
    secondaryTitle: "Query safety",
    secondary: () => [
      card("Guards", "SQL is an operational read surface, not a migration surface.", [
        { label: "Read-only role", value: "enabled", tone: "ok" },
        { label: "Timeout", value: "10 s", tone: "running" },
        { label: "Export audit", value: "recorded", tone: "ok" },
      ]),
    ],
  },
  "/build/data/memory": {
    adapter: "AgentField memory",
    primaryAction: "Search memory",
    controls: buildControls,
    kpis: (snapshot) => [
      makeKpi("Scopes", String(new Set(snapshot.memory.map((item) => item.primary)).size), "tenant + agent", "mapped", "ok"),
      makeKpi("Entries", String(snapshot.memory.length), "active", "live", "neutral"),
      makeKpi("Vectorized", String(snapshot.memory.filter((item) => item.metric.includes("vector")).length), "searchable", "live", "ok"),
      makeKpi("Stale", "0", "needs compaction", "live", "ok"),
    ],
    tableTitle: "Memory scopes",
    tableDescription: "Memory namespaces, vector coverage, retention rules, and query probes.",
    rows: (snapshot) => snapshot.memory,
    secondaryTitle: "Retrieval probes",
    secondary: () => [
      card("Quality checks", "Operator-visible memory needs both coverage and trust boundaries.", [
        { label: "Tenant isolation", value: "enforced", tone: "ok" },
        { label: "Search sample", value: "available", tone: "running" },
        { label: "Delete path", value: "required", tone: "warn" },
      ]),
    ],
  },
  "/build/data/storage": {
    adapter: "S3 / MinIO",
    primaryAction: "Create signed URL",
    controls: buildControls,
    kpis: (snapshot) => [
      makeKpi("Objects", String(snapshot.storage.length), "tenant prefixed", "live", "neutral"),
      makeKpi("Storage", storageTotal(snapshot.storage), "current", "live", "running"),
      makeKpi("Signed URLs", "0", "last hour", "live", "neutral"),
      makeKpi("Policy drift", "0", "checks", "clean", "ok"),
    ],
    tableTitle: "Object storage",
    tableDescription: "Tenant-prefixed buckets, object keys, signed URL activity, and retention posture.",
    rows: (snapshot) => snapshot.storage,
    secondaryTitle: "Storage policy",
    secondary: () => [
      card("Access model", "Storage access should flow through signed URLs and audited operations.", [
        { label: "Tenant prefix", value: "required", tone: "ok" },
        { label: "Public buckets", value: "0", tone: "ok" },
        { label: "Default expiry", value: "15 min", tone: "running" },
      ]),
    ],
  },
  "/build/data/search": {
    adapter: "Search index",
    primaryAction: "Run retrieval probe",
    controls: buildControls,
    kpis: () => [
      makeKpi("Indexes", "6", "tenant scoped", "current", "ok"),
      makeKpi("Indexed docs", "14.2k", "chunks", "+260", "neutral"),
      makeKpi("Freshness p95", "42 s", "ingest to search", "-6 s", "ok"),
      makeKpi("Probe pass", "91%", "sample set", "+3 pts", "ok"),
    ],
    tableTitle: "Search indexes",
    tableDescription: "Index health, ingestion freshness, retrieval quality, and sample queries.",
    rows: (snapshot) => snapshot.searchIndexes,
    secondaryTitle: "Retrieval quality",
    secondary: () => [
      card("Probe set", "Retrieval checks should be concrete enough for operators to trust search-backed agents.", [
        { label: "Golden queries", value: "42" },
        { label: "Min hit score", value: "0.78", tone: "ok" },
        { label: "Stale docs", value: "11", tone: "warn" },
      ]),
    ],
  },
  "/build/modules": {
    adapter: "Module registry",
    primaryAction: "Open module",
    controls: buildControls,
    kpis: () => [
      makeKpi("Modules", "8", "mounted", "current", "ok"),
      makeKpi("Routes", "37", "owned", "+4", "neutral"),
      makeKpi("Migrations", "0", "pending", "clean", "ok"),
      makeKpi("Workers", "5", "module queues", "running", "running"),
    ],
    tableTitle: "Runtime modules",
    tableDescription: "Code-owned extension points, routes, background jobs, and migration state.",
    rows: (snapshot) => snapshot.modules,
    secondaryTitle: "Module contract",
    secondary: () => [
      card("Code ownership", "Modules should expose their routes, jobs, tables, and operator pages explicitly.", [
        { label: "Routes", value: "declared", tone: "ok" },
        { label: "Tables", value: "declared", tone: "ok" },
        { label: "Jobs", value: "declared", tone: "ok" },
      ]),
    ],
  },
  "/build/skills": {
    adapter: "MCP",
    primaryAction: "Test skill",
    controls: buildControls,
    kpis: () => [
      makeKpi("Servers", "5", "connected", "current", "ok"),
      makeKpi("Tools", "42", "available", "+3", "neutral"),
      makeKpi("Skill tests", "18", "passing", "clean", "ok"),
      makeKpi("Disabled", "2", "needs auth", "review", "warn"),
    ],
    tableTitle: "Skills and MCP servers",
    tableDescription: "Installed tool servers, exposed tools, auth posture, and last probe.",
    rows: (snapshot) => snapshot.skills,
    secondaryTitle: "Tool safety",
    secondary: () => [
      card("Operator rules", "Skills are powerful enough to need visible auth and blast-radius indicators.", [
        { label: "Auth scope", value: "visible", tone: "ok" },
        { label: "Last probe", value: "visible", tone: "ok" },
        { label: "Disable switch", value: "required", tone: "running" },
      ]),
    ],
  },
  "/build/feature-flags": {
    adapter: "Runtime config",
    primaryAction: "Create flag",
    controls: buildControls,
    kpis: (snapshot) => [
      makeKpi("Flags", String(snapshot.featureFlags.length), "active", "live", "neutral"),
      makeKpi("Enabled", String(snapshot.featureFlags.filter((item) => item.status === "enabled").length), "runtime", "live", "running"),
      makeKpi("Overrides", String(snapshot.featureFlags.filter((item) => item.secondary.includes("db")).length), "tenant scoped", "live", "warn"),
      makeKpi("Incidents", "0", "last 7d", "clean", "ok"),
    ],
    tableTitle: "Feature flags",
    tableDescription: "Runtime flags, tenant overrides, rollout stage, and audit history.",
    rows: (snapshot) => snapshot.featureFlags,
    secondaryTitle: "Rollout control",
    secondary: () => [
      card("Guardrails", "Every flag should be reversible and visible in audit.", [
        { label: "Kill switch", value: "required", tone: "ok" },
        { label: "Tenant override", value: "supported", tone: "running" },
        { label: "Audit", value: "recorded", tone: "ok" },
      ]),
    ],
  },
  "/customers/tenants": {
    adapter: "Admin API",
    primaryAction: "Create tenant",
    controls: customerControls,
    kpis: () => [
      makeKpi("Tenants", "3", "active", "+1", "neutral"),
      makeKpi("Healthy", "2", "no warnings", "current", "ok"),
      makeKpi("Near budget", "1", "beta-labs", "watch", "warn"),
      makeKpi("Deleted", "0", "current", "clean", "ok"),
    ],
    tableTitle: "Tenant workspaces",
    tableDescription: "Customer workspaces with isolation, health, members, keys, budgets, and drilldowns.",
    rows: (snapshot) => snapshot.tenants,
    secondaryTitle: "Tenant detail",
    secondary: () => [
      card("Drilldown route", "Tenant detail pages consolidate keys, users, spend, and audit.", [
        { label: "Route", value: "/customers/tenants/{id}" },
        { label: "Budget", value: "inline", tone: "running" },
        { label: "Isolation", value: "visible", tone: "ok" },
      ]),
    ],
  },
  "/customers/api-keys": {
    adapter: "Admin API",
    primaryAction: "Issue API key",
    controls: customerControls,
    kpis: () => [
      makeKpi("Active keys", "6", "all tenants", "+1", "neutral"),
      makeKpi("Rotation due", "1", "next 7d", "watch", "warn"),
      makeKpi("Revoked", "0", "current", "clean", "ok"),
      makeKpi("Spend linked", "100%", "metered keys", "clean", "ok"),
    ],
    tableTitle: "API keys",
    tableDescription: "Tenant keys, prefixes, limits, spend, rotation, and revocation state.",
    rows: (snapshot) => snapshot.apiKeys,
    secondaryTitle: "Key policy",
    secondary: () => [
      card("Defaults", "New keys should be safe by default and useful for developers.", [
        { label: "Prefix", value: "visible", tone: "ok" },
        { label: "Secret", value: "one-time", tone: "ok" },
        { label: "Rate limit", value: "required", tone: "running" },
      ]),
    ],
  },
  "/customers/members": {
    adapter: "Admin API",
    primaryAction: "Invite member",
    controls: customerControls,
    kpis: () => [
      makeKpi("Members", "10", "active", "+2", "neutral"),
      makeKpi("Owners", "3", "one per tenant", "ok", "ok"),
      makeKpi("Invites", "2", "pending", "watch", "running"),
      makeKpi("Deactivated", "0", "current", "clean", "ok"),
    ],
    tableTitle: "Members",
    tableDescription: "Users and memberships across tenants with role and session context.",
    rows: (snapshot) => snapshot.members,
    secondaryTitle: "Access model",
    secondary: () => [
      card("Roles", "Membership roles should be clear enough for support and billing operations.", [
        { label: "Owner", value: "tenant admin", tone: "ok" },
        { label: "Admin", value: "manage keys", tone: "running" },
        { label: "Member", value: "runtime access" },
      ]),
    ],
  },
  "/customers/budgets": {
    adapter: "Billing meter",
    primaryAction: "Set budget",
    controls: customerControls,
    kpis: (snapshot) => pickKpis(snapshot, [3, 7, 2, 1]),
    tableTitle: "Budgets",
    tableDescription: "Tenant caps, spend usage, thresholds, and alert posture for the current period.",
    rows: budgetRows,
    secondaryTitle: "Budget automation",
    secondary: (snapshot) => [
      card("Thresholds", "Default threshold behavior for all configured budgets.", [
        { label: "Warn", value: "80%", tone: "warn" },
        { label: "Block", value: "100%", tone: "fail" },
        { label: "Notification", value: "email + webhook", tone: "running" },
      ]),
      card("Current tenants", "Usage should be visible before a cap is changed.", snapshot.budgets.map((budget) => ({
        label: budget.tenant,
        value: `${budget.used}%`,
        tone: budget.status,
      }))),
    ],
  },
  "/customers/audit": {
    adapter: "Audit log",
    primaryAction: "Export audit",
    controls: customerControls,
    kpis: () => [
      makeKpi("Events", "184", "24h", "+12", "neutral"),
      makeKpi("Key changes", "7", "24h", "normal", "running"),
      makeKpi("Budget edits", "2", "24h", "review", "warn"),
      makeKpi("Integrity", "ok", "append-only", "clean", "ok"),
    ],
    tableTitle: "Audit log",
    tableDescription: "Operator and system mutations with actor, resource, tenant, and source links.",
    rows: (snapshot) => snapshot.audit,
    secondaryTitle: "Audit policy",
    secondary: () => [
      card("Required fields", "Entries need enough context to reconstruct operator decisions.", [
        { label: "Actor", value: "required", tone: "ok" },
        { label: "Tenant", value: "required when scoped", tone: "ok" },
        { label: "Diff", value: "preferred", tone: "running" },
      ]),
    ],
  },
  "/customers/billing": {
    adapter: "Lago / Stripe",
    primaryAction: "Sync billing",
    controls: customerControls,
    kpis: () => [
      makeKpi("Customers", "3", "linked", "current", "ok"),
      makeKpi("Meters", "5", "active", "current", "ok"),
      makeKpi("Uninvoiced", "$184", "current period", "+$21", "running"),
      makeKpi("Sync lag", "4 min", "provider", "normal", "ok"),
    ],
    tableTitle: "Billing summary",
    tableDescription: "Customer records, meters, invoices, portal links, and sync state.",
    rows: (snapshot) => snapshot.billing,
    secondaryTitle: "Billing adapters",
    secondary: () => [
      card("Adapter contract", "Billing can be swapped, but operator surfaces should stay stable.", [
        { label: "Customer sync", value: "required", tone: "ok" },
        { label: "Meter sync", value: "required", tone: "ok" },
        { label: "Portal link", value: "optional", tone: "neutral" },
      ]),
    ],
  },
  "/setup/adapters": {
    adapter: "Adapter registry",
    primaryAction: "Test adapters",
    controls: setupControls,
    kpis: () => [
      makeKpi("Slots", "10", "swappable", "mapped", "ok"),
      makeKpi("Ready", "7", "configured", "current", "ok"),
      makeKpi("Warnings", "1", "Svix local", "review", "warn"),
      makeKpi("Optional", "2", "billing, notifications", "neutral", "neutral"),
    ],
    tableTitle: "Adapter slots",
    tableDescription: "Every backend capability slot and its currently wired implementation.",
    rows: adapterRows,
    secondaryTitle: "Adapter readiness",
    secondary: (snapshot) => [
      card("Slots", "Configured backend slots should be visible before backend hardening.", snapshot.adapters.slice(0, 6).map((adapter) => ({
        label: adapter.slot,
        value: adapter.adapter,
        tone: adapter.status,
      }))),
    ],
  },
  "/setup/auth-providers": {
    adapter: "Better Auth",
    primaryAction: "Test sign-in",
    controls: setupControls,
    kpis: () => [
      makeKpi("Providers", "2", "email + oauth", "configured", "ok"),
      makeKpi("Sessions", "14", "active", "normal", "running"),
      makeKpi("MFA", "optional", "operator", "review", "warn"),
      makeKpi("Invite flow", "on", "customers", "ready", "ok"),
    ],
    tableTitle: "Auth providers",
    tableDescription: "Operator sessions, customer signups, provider readiness, and tenant membership links.",
    rows: () => [
      row("auth-email", "Email link", "Passwordless operator and customer sign-in.", "enabled", "ok", "primary", "current"),
      row("auth-google", "Google OAuth", "Workspace-friendly customer signup.", "enabled", "ok", "oauth", "current"),
      row("auth-github", "GitHub OAuth", "Developer account linking.", "planned", "neutral", "oauth", "backlog"),
      row("auth-mfa", "MFA enforcement", "Optional for operators, tenant policy later.", "partial", "warn", "policy", "review"),
    ],
    secondaryTitle: "Session policy",
    secondary: () => [
      card("Defaults", "Session controls should be predictable for operators.", [
        { label: "Operator session", value: "7 days", tone: "running" },
        { label: "Customer session", value: "14 days" },
        { label: "Audit sign-ins", value: "on", tone: "ok" },
      ]),
    ],
  },
  "/setup/llm-providers": {
    adapter: "LiteLLM",
    primaryAction: "Test model route",
    controls: setupControls,
    kpis: () => [
      makeKpi("Providers", "4", "openai, anthropic, google", "configured", "ok"),
      makeKpi("Models", "21", "available", "current", "ok"),
      makeKpi("Fallbacks", "3", "routes", "ready", "running"),
      makeKpi("Rate limit", "0", "active blocks", "clean", "ok"),
    ],
    tableTitle: "LLM provider routes",
    tableDescription: "Model routing, fallback, provider health, and spend recording posture.",
    rows: (snapshot) => snapshot.llmProviders,
    secondaryTitle: "Routing policy",
    secondary: () => [
      card("Fallback chain", "Provider routing should explain what happens when a model is unavailable.", [
        { label: "Primary", value: "agent configured", tone: "running" },
        { label: "Fallback", value: "same class" },
        { label: "Cost ledger", value: "always on", tone: "ok" },
      ]),
    ],
  },
  "/setup/sandbox": {
    adapter: "E2B / local",
    primaryAction: "Run sandbox probe",
    controls: setupControls,
    kpis: () => [
      makeKpi("Capacity", "6", "concurrent", "normal", "ok"),
      makeKpi("Cold start", "1.4 s", "p50", "-0.2 s", "ok"),
      makeKpi("Artifacts", "on", "S3 backed", "ready", "ok"),
      makeKpi("Network policy", "restricted", "default", "safe", "running"),
    ],
    tableTitle: "Sandbox adapter",
    tableDescription: "Code execution capacity, network policy, artifacts, and provider probe results.",
    rows: (snapshot) => snapshot.sandbox,
    secondaryTitle: "Execution policy",
    secondary: () => [
      card("Limits", "Sandbox limits should be visible before a user hits them.", [
        { label: "CPU", value: "2 vCPU" },
        { label: "Memory", value: "4 GB" },
        { label: "Timeout", value: "10 min", tone: "running" },
      ]),
    ],
  },
  "/setup/webhook-subscribers": {
    adapter: "Svix",
    primaryAction: "Add endpoint",
    controls: setupControls,
    kpis: () => [
      makeKpi("Endpoints", "7", "subscribed", "+1", "neutral"),
      makeKpi("Events", "12", "published", "current", "ok"),
      makeKpi("Signing", "on", "all endpoints", "safe", "ok"),
      makeKpi("Failed probes", "1", "needs replay", "watch", "warn"),
    ],
    tableTitle: "Webhook subscribers",
    tableDescription: "Outbound endpoints, event subscriptions, signing, retries, and delivery posture.",
    rows: (snapshot) => snapshot.webhookEndpoints,
    secondaryTitle: "Endpoint policy",
    secondary: () => [
      card("Delivery defaults", "Subscriber state should match delivery log behavior.", [
        { label: "Signing", value: "required", tone: "ok" },
        { label: "Retries", value: "72h", tone: "running" },
        { label: "Replay", value: "operator action", tone: "running" },
      ]),
    ],
  },
  "/setup/notifications": {
    adapter: "Resend / log",
    primaryAction: "Send test",
    controls: setupControls,
    kpis: () => [
      makeKpi("Channels", "3", "email, webhook, log", "mapped", "ok"),
      makeKpi("Templates", "9", "operator/customer", "+1", "neutral"),
      makeKpi("Failures", "0", "last 24h", "clean", "ok"),
      makeKpi("Outbox", "4", "queued", "normal", "running"),
    ],
    tableTitle: "Notification channels",
    tableDescription: "Outbox adapters, templates, routing rules, and test delivery results.",
    rows: (snapshot) => snapshot.notifications,
    secondaryTitle: "Template state",
    secondary: () => [
      card("Required templates", "Customer-facing system events should not ship as raw logs.", [
        { label: "Invite", value: "ready", tone: "ok" },
        { label: "Budget warning", value: "ready", tone: "ok" },
        { label: "Run failure", value: "draft", tone: "running" },
      ]),
    ],
  },
  "/setup/secrets": {
    adapter: "Vault",
    primaryAction: "Rotate secret",
    controls: setupControls,
    kpis: (snapshot) => [
      makeKpi("Secrets", String(snapshot.secrets.length), "configured", "live", "ok"),
      makeKpi("Due rotation", String(snapshot.secrets.filter((item) => item.status.includes("rotation")).length), "next 30d", "live", "warn"),
      makeKpi("Tenant overrides", String(snapshot.secrets.filter((item) => item.secondary.includes("tenant")).length), "scoped", "live", "running"),
      makeKpi("Plaintext", "0", "visible", "clean", "ok"),
    ],
    tableTitle: "Secrets",
    tableDescription: "Provider keys, rotation windows, tenant overrides, and last access posture.",
    rows: (snapshot) => snapshot.secrets,
    secondaryTitle: "Secret policy",
    secondary: () => [
      card("Visibility", "Operators need state, not secret values.", [
        { label: "Plaintext display", value: "never", tone: "ok" },
        { label: "Rotation audit", value: "required", tone: "ok" },
        { label: "Tenant override", value: "scoped", tone: "running" },
      ]),
    ],
  },
  "/setup/observability": {
    adapter: "OpenTelemetry",
    primaryAction: "Open dashboards",
    controls: setupControls,
    kpis: () => [
      makeKpi("Logs", "enabled", "runtime", "current", "ok"),
      makeKpi("Metrics", "enabled", "prometheus", "current", "ok"),
      makeKpi("Traces", "enabled", "100% local", "current", "ok"),
      makeKpi("Alerts", "5", "configured", "normal", "running"),
    ],
    tableTitle: "Observability sinks",
    tableDescription: "Logs, metrics, traces, alerts, and external dashboard links.",
    rows: (snapshot) => snapshot.observability,
    secondaryTitle: "Signal links",
    secondary: () => [
      card("Dashboard links", "Operator pages should deep-link to external observability when configured.", [
        { label: "Logs", value: "available", tone: "ok" },
        { label: "Metrics", value: "available", tone: "ok" },
        { label: "Traces", value: "internal + external", tone: "running" },
      ]),
    ],
  },
  "/setup/billing-adapter": {
    adapter: "Lago / Stripe",
    primaryAction: "Test billing sync",
    controls: setupControls,
    kpis: () => [
      makeKpi("Adapter", "Stripe", "active", "configured", "ok"),
      makeKpi("Meters", "5", "mapped", "current", "ok"),
      makeKpi("Sync jobs", "3", "running", "normal", "running"),
      makeKpi("Failures", "0", "24h", "clean", "ok"),
    ],
    tableTitle: "Billing adapter",
    tableDescription: "Customer sync, meter mappings, invoice state, and provider readiness.",
    rows: (snapshot) => snapshot.billing,
    secondaryTitle: "Meter contract",
    secondary: () => [
      card("Required meters", "Billing should explain exactly what usage becomes money.", [
        { label: "Requests", value: "metered", tone: "ok" },
        { label: "Tokens", value: "metered", tone: "ok" },
        { label: "Storage", value: "optional", tone: "neutral" },
      ]),
    ],
  },
  "/setup/deploy-targets": {
    adapter: "Deploy config",
    primaryAction: "Run deploy check",
    controls: setupControls,
    kpis: () => [
      makeKpi("Targets", "5", "runtime surfaces", "mapped", "ok"),
      makeKpi("Ready", "4", "configured", "current", "ok"),
      makeKpi("Missing env", "1", "billing optional", "review", "warn"),
      makeKpi("Last check", "2m", "ago", "fresh", "ok"),
    ],
    tableTitle: "Deploy targets",
    tableDescription: "Runtime, dashboard, worker, storage, and webhook deployment surfaces.",
    rows: (snapshot) => snapshot.deployTargets,
    secondaryTitle: "Release checklist",
    secondary: () => [
      card("Before deploy", "A deploy target page should make missing environment pieces obvious.", [
        { label: "Runtime health", value: "required", tone: "ok" },
        { label: "Worker health", value: "required", tone: "ok" },
        { label: "Webhook reachability", value: "required", tone: "running" },
      ]),
    ],
  },
  "/brand": {
    adapter: "Theme file",
    primaryAction: "Preview brand",
    controls: [
      { kind: "select", label: "Surface", value: "Admin", options: ["Admin", "Customer app", "Docs"] },
      { kind: "tabs", label: "Mode", value: "dark", options: ["dark", "light", "system"] },
      { kind: "switch", label: "Monochrome", value: "on" },
    ],
    kpis: () => [
      makeKpi("Radius", "6px", "base token", "locked", "ok"),
      makeKpi("Cards", "8px", "max radius", "locked", "ok"),
      makeKpi("Palette", "zinc", "monochrome", "locked", "ok"),
      makeKpi("Motion", "150ms", "standard", "locked", "running"),
    ],
    tableTitle: "Brand tokens",
    tableDescription: "Operator-owned identity, shell tokens, type scale, and public naming assets.",
    rows: () => [
      row("brand-name", "BackAI Studio", "Admin app wordmark and product shell name.", "active", "ok", "text", "locked"),
      row("brand-radius", "Radius", "Base 6px, cards 8px, compact controls.", "active", "ok", "6px", "locked"),
      row("brand-palette", "Palette", "Zinc monochrome with semantic status only.", "active", "ok", "zinc", "locked"),
      row("brand-type", "Typography", "Geist Sans with Geist Mono for data values.", "active", "ok", "2 fonts", "locked"),
      row("brand-motion", "Motion", "120-150ms controls, 200ms drawers.", "active", "running", "tokens", "locked"),
    ],
    secondaryTitle: "Design rules",
    secondary: () => [
      card("Layout grammar", "These values come from the design package and should stay centralized.", [
        { label: "Top bar", value: "48px", tone: "ok" },
        { label: "Sidebar", value: "206px", tone: "ok" },
        { label: "Page padding", value: "24px", tone: "ok" },
        { label: "Rows", value: "32-40px", tone: "running" },
      ]),
      card("Color use", "Status color is semantic; navigation and hierarchy stay monochrome.", [
        { label: "OK", value: "green", tone: "ok" },
        { label: "Warn", value: "amber", tone: "warn" },
        { label: "Fail", value: "red", tone: "fail" },
      ]),
    ],
  },
}

function dynamicAgentPage(pathname: string, snapshot: OperatorSnapshot): PageModel | null {
  const match = pathname.match(/^\/build\/agents\/([^/]+)(\/playground)?$/)
  if (!match) return null
  const agentId = decodeURIComponent(match[1])
  const agent = snapshot.agents.find((item) => item.id === agentId || item.primary === agentId) ?? snapshot.agents[0]
  const playground = Boolean(match[2])

  return {
    path: pathname,
    group: "Build",
    title: playground ? `${agentId} playground` : agentId,
    description: playground
      ? "Run controlled test prompts against the selected agent with trace, cost, and output inspection."
      : "Agent detail with reasoner graph, runtime health, versions, cost posture, and recent executions.",
    live: snapshot.source === "live",
    generatedAt: snapshot.generatedAt,
    adapter: "AgentField runtime",
    primaryAction: playground ? "Run test" : "Open playground",
    controls: buildControls,
    kpis: playground
      ? [
          makeKpi("Test runs", "12", "today", "+3", "running"),
          makeKpi("Last status", "ok", "streamed", "clean", "ok"),
          makeKpi("Avg cost", "$0.11", "sample set", "-4%", "ok"),
          makeKpi("Avg latency", "1.8 s", "sample set", "stable", "running"),
        ]
      : [
          makeKpi("Runs", agent?.metric ?? "0", "selected agent", "runtime", agent?.tone ?? "neutral"),
          makeKpi("Version", agent?.timestamp ?? "n/a", "deployed", "current", "running"),
          makeKpi("Health", agent?.status ?? "unknown", "runtime", "current", agent?.tone ?? "neutral"),
          makeKpi("Cost today", "$14.82", "agent scoped", "+6%", "neutral"),
        ],
    tableTitle: playground ? "Playground runs" : "Agent internals",
    tableDescription: playground
      ? "Recent manual test inputs, output status, traces, and cost for this agent."
      : "Reasoners, tools, memory scopes, data access, and recent execution context for this agent.",
    tableColumns: defaultColumns,
    rows: playground
      ? [
          row("pg-smoke", "Smoke prompt", "Short happy-path prompt with streaming enabled.", "succeeded", "ok", "$0.03 · 0.8 s", "just now"),
          row("pg-edge", "Edge prompt", "Missing context, expects refusal or clarification.", "succeeded", "ok", "$0.04 · 1.1 s", "4m ago"),
          row("pg-long", "Long context prompt", "Retrieval and memory attached.", "warning", "warn", "$0.16 · 4.2 s", "18m ago"),
        ]
      : [
          row("agent-summary", agent?.primary ?? agentId, agent?.secondary ?? "Runtime manifest entry.", agent?.status ?? "unknown", agent?.tone ?? "neutral", agent?.metric ?? "n/a", agent?.timestamp ?? "runtime"),
          row("agent-reasoners", "Reasoner graph", agent?.secondary ?? "Configured reasoners.", "ready", "ok", "3 nodes", "runtime"),
          row("agent-memory", "Memory scope", `${agentId}/default`, "available", "running", "searchable", "current"),
          row("agent-tools", "Tool access", "MCP and internal tools exposed to the graph.", "guarded", "warn", "5 tools", "current"),
          row("agent-runs", "Recent executions", "Filtered run stream for this agent.", "linked", "ok", agent?.metric ?? "0 runs", "live"),
        ],
    secondaryTitle: playground ? "Test harness" : "Agent controls",
    secondary: playground
      ? [
          card("Input schema", "Playground form should be generated from the agent input contract.", [
            { label: "Schema", value: "available", tone: "ok" },
            { label: "Streaming", value: "toggleable", tone: "running" },
            { label: "Trace link", value: "required", tone: "ok" },
          ]),
          card("Sample request", "Minimal request shape for the selected agent.", undefined, `POST /api/v1/agents/${agentId}/runs`),
        ]
      : [
          card("Lifecycle", "Operational controls for the selected agent.", [
            { label: "Deploy version", value: agent?.timestamp ?? "runtime" },
            { label: "Rollback", value: "available", tone: "running" },
            { label: "Disable", value: "guarded", tone: "warn" },
          ]),
          card("Links", "Detail routes operators expect to jump between.", [
            { label: "Playground", value: `/build/agents/${agentId}/playground` },
            { label: "Runs", value: "/operate/runs" },
            { label: "Traces", value: "/operate/traces" },
          ]),
        ],
  }
}

function dynamicTenantPage(pathname: string, snapshot: OperatorSnapshot): PageModel | null {
  const match = pathname.match(/^\/customers\/tenants\/([^/]+)$/)
  if (!match) return null
  const tenantId = decodeURIComponent(match[1])
  const tenant = snapshot.tenants.find((item) => item.id === tenantId || item.primary === tenantId) ?? snapshot.tenants[0]
  const budget = snapshot.budgets.find((item) => item.tenant === tenantId) ?? snapshot.budgets[0]

  return {
    path: pathname,
    group: "Customers",
    title: tenant?.primary ?? tenantId,
    description: "Tenant detail with workspace health, keys, members, budget, recent runs, and audit entries.",
    live: snapshot.source === "live",
    generatedAt: snapshot.generatedAt,
    adapter: "Admin API",
    primaryAction: "Manage tenant",
    controls: customerControls,
    kpis: [
      makeKpi("Budget used", `${budget?.used ?? 0}%`, budget?.cap ?? "no cap", "current", budget?.status ?? "neutral"),
      makeKpi("API keys", String(snapshot.apiKeys.length), "active keys", "current", "ok"),
      makeKpi("Members", String(snapshot.members.length), "active members", "current", "ok"),
      makeKpi("Runs", String(snapshot.runs.length), "recent executions", "live", "running"),
    ],
    tableTitle: "Tenant workspace",
    tableDescription: "Workspace objects that support a customer conversation without jumping across pages.",
    tableColumns: defaultColumns,
    rows: [
      row("tenant-summary", tenant?.primary ?? tenantId, tenant?.secondary ?? "Tenant workspace.", tenant?.status ?? "active", tenant?.tone ?? "neutral", tenant?.metric ?? "n/a", tenant?.timestamp ?? "current"),
      ...snapshot.apiKeys.slice(0, 2).map((key) => ({ ...key, id: `tenant-${key.id}` })),
      ...snapshot.members.slice(0, 2).map((member) => ({ ...member, id: `tenant-${member.id}` })),
      ...snapshot.runs.slice(0, 2).map((run) => ({ ...run, id: `tenant-${run.id}` })),
    ],
    secondaryTitle: "Tenant controls",
    secondary: [
      card("Isolation", "Tenant detail should make boundaries and spend visible.", [
        { label: "Tenant ID", value: tenantId },
        { label: "Budget", value: `${budget?.used ?? 0}%`, tone: budget?.status ?? "neutral" },
        { label: "Audit", value: "linked", tone: "ok" },
      ]),
      card("Common actions", "Actions remain guarded because they can affect a customer workspace.", [
        { label: "Issue key", value: "available", tone: "running" },
        { label: "Set budget", value: "available", tone: "running" },
        { label: "Delete tenant", value: "requires confirmation", tone: "fail" },
      ]),
    ],
  }
}

export function buildPageModel(pathname: string, snapshot: OperatorSnapshot): PageModel | null {
  const normalized = normalizePath(pathname)
  const dynamic = dynamicAgentPage(normalized, snapshot) ?? dynamicTenantPage(normalized, snapshot)
  if (dynamic) return dynamic

  const definition = definitions[normalized]
  if (!definition) return null

  const navItem = navItemForPath(normalized)
  const listed = allNavItems.some((item) => item.href === normalized)
  if (!listed) return null

  return {
    path: normalized,
    group: groupForPath(normalized),
    title: navItem.title,
    description: navItem.description,
    live: snapshot.source === "live",
    generatedAt: snapshot.generatedAt,
    adapter: definition.adapter,
    primaryAction: definition.primaryAction,
    controls: definition.controls ?? platformControls,
    kpis: definition.kpis ? definition.kpis(snapshot) : pickKpis(snapshot, [0, 1, 2, 3]),
    tableTitle: definition.tableTitle,
    tableDescription: definition.tableDescription,
    tableColumns: definition.tableColumns ?? defaultColumns,
    rows: definition.rows(snapshot),
    secondaryTitle: definition.secondaryTitle,
    secondary: definition.secondary(snapshot),
  }
}
