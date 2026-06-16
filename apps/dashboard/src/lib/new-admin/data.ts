// SPDX-License-Identifier: Apache-2.0

import { api, type LogCapabilities, type MetricsCapabilities, type MetricsInstantResponse, type MetricsRangeResponse, type TraceCapabilities, type TraceDetail } from "@/lib/api"

export type HealthSource = "live" | "seeded"

export type StatusTone = "ok" | "running" | "warn" | "fail" | "neutral"

export type Kpi = {
  label: string
  value: string
  detail: string
  trend: string
  tone?: StatusTone
  sparkline: number[]
}

export type ConsoleRow = {
  id: string
  primary: string
  secondary: string
  status: string
  tone: StatusTone
  metric: string
  timestamp: string
  href?: string
  detail?: string
}

export type ServiceVital = {
  name: string
  version: string
  status: "healthy" | "degraded" | "offline" | "configured"
  checked: string
  adapter?: string
  href?: string
}

export type AdapterSlot = {
  slot: string
  adapter: string
  status: StatusTone
  description: string
}

export type BudgetRecord = {
  tenant: string
  cap: string
  used: number
  status: StatusTone
}

export type OperatorSnapshot = {
  source: HealthSource
  generatedAt: string
  runtimeStatus: string
  endpointStatus: Record<string, "ok" | "missing" | "degraded">
  kpis: Kpi[]
  activity: ConsoleRow[]
  runs: ConsoleRow[]
  costRows: ConsoleRow[]
  tenants: ConsoleRow[]
  apiKeys: ConsoleRow[]
  members: ConsoleRow[]
  agents: ConsoleRow[]
  tables: ConsoleRow[]
  dbHealth: ConsoleRow[]
  queue: ConsoleRow[]
  errors: ConsoleRow[]
  traces: ConsoleRow[]
  logs: ConsoleRow[]
  cache: ConsoleRow[]
  webhooks: ConsoleRow[]
  webhookEndpoints: ConsoleRow[]
  approvals: ConsoleRow[]
  audit: ConsoleRow[]
  oauth: ConsoleRow[]
  sessions: ConsoleRow[]
  jobs: ConsoleRow[]
  jobDefinitions: ConsoleRow[]
  storage: ConsoleRow[]
  memory: ConsoleRow[]
  searchIndexes: ConsoleRow[]
  modules: ConsoleRow[]
  skills: ConsoleRow[]
  tools: ConsoleRow[]
  reasoners: ConsoleRow[]
  harnesses: ConsoleRow[]
  crons: ConsoleRow[]
  featureFlags: ConsoleRow[]
  billing: ConsoleRow[]
  secrets: ConsoleRow[]
  notifications: ConsoleRow[]
  notificationMutes: ConsoleRow[]
  llmProviders: ConsoleRow[]
  providerHealth: ConsoleRow[]
  sandbox: ConsoleRow[]
  observability: ConsoleRow[]
  deployTargets: ConsoleRow[]
  services: ServiceVital[]
  brand: ConsoleRow[]
  adapters: AdapterSlot[]
  logCapabilities: LogCapabilities
  traceCapabilities: TraceCapabilities
  metricsCapabilities: MetricsCapabilities
  costSeries: ConsoleRow[]
  containerMetrics: ConsoleRow[]
  features: ConsoleRow[]
  featureWarnings: ConsoleRow[]
  budgets: BudgetRecord[]
  snippets: {
    runtimeUrl: string
    tenantKey: string
    curl: string
    typescript: string
    python: string
    go: string
  }
}

async function settle<T>(producer: () => Promise<T>): Promise<T | null> {
  try {
    return await producer()
  } catch {
    return null
  }
}

function nowIso() {
  return new Date().toISOString()
}

function shortTime(value?: string | null) {
  if (!value) return "now"
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleTimeString("en", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  })
}

function money(value?: number | null) {
  return `$${(value ?? 0).toLocaleString("en", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`
}

function duration(ms?: number | null) {
  if (!ms) return "0 ms"
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${(ms / 1000).toFixed(1)} s`
}

function bytes(value?: number | null) {
  const n = value ?? 0
  if (n >= 1024 * 1024 * 1024) return `${(n / 1024 / 1024 / 1024).toFixed(1)} GiB`
  if (n >= 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MiB`
  if (n >= 1024) return `${Math.round(n / 1024).toLocaleString("en")} KiB`
  return `${n.toLocaleString("en")} B`
}

function metricLabel(metric: Record<string, string>) {
  return metric.name ?? metric.__name__ ?? metric.job ?? metric.container_label_com_docker_compose_service ?? metric.name ?? "series"
}

function metricRangeRows(range: MetricsRangeResponse | null | undefined, kind: "cost" | "cpu" | "restart"): ConsoleRow[] {
  if (!range?.series.length) return []
  return range.series.slice(0, 8).map((series, index) => {
    const latest = series.values[series.values.length - 1]?.value ?? 0
    const label = metricLabel(series.metric)
    return {
      id: `${kind}-${index}-${label}`,
      primary: label,
      secondary: Object.entries(series.metric).filter(([key]) => key !== "__name__").map(([key, value]) => `${key}=${value}`).join(" · ") || "promql series",
      status: kind === "cost" ? "metrics" : "container",
      tone: "running" as StatusTone,
      metric: kind === "cost" ? money(latest) : kind === "cpu" ? `${latest.toFixed(3)} cores` : `${Math.round(latest).toLocaleString("en")} restarts`,
      timestamp: series.values[series.values.length - 1]?.ts ? shortTime(series.values[series.values.length - 1].ts) : "current",
    }
  })
}

function metricInstantRows(response: MetricsInstantResponse | null | undefined, kind: "memory"): ConsoleRow[] {
  if (!response?.samples.length) return []
  return response.samples.slice(0, 8).map((sample, index) => {
    const label = metricLabel(sample.metric)
    return {
      id: `${kind}-${index}-${label}`,
      primary: label,
      secondary: Object.entries(sample.metric).filter(([key]) => key !== "__name__").map(([key, value]) => `${key}=${value}`).join(" · ") || "promql sample",
      status: "container",
      tone: "running" as StatusTone,
      metric: bytes(sample.value),
      timestamp: shortTime(sample.ts),
    }
  })
}

function displayValue(value: unknown) {
  if (value == null) return "not set"
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return String(value)
  }
  try {
    return JSON.stringify(value)
  } catch {
    return "structured value"
  }
}

function toneForStatus(status: string): StatusTone {
  const normalized = status.toLowerCase()
  if (["healthy", "ok", "succeeded", "delivered", "completed", "sent", "enabled", "configured"].includes(normalized)) {
    return "ok"
  }
  if (["running", "queued", "pending", "retryable", "sending", "available"].includes(normalized)) {
    return "running"
  }
  if (["warning", "warn", "near", "timeout", "scheduled", "skipped"].includes(normalized)) {
    return "warn"
  }
  if (["failed", "critical", "offline", "over", "discarded", "cancelled"].includes(normalized)) {
    return "fail"
  }
  return "neutral"
}

function toneForAdapterStatus(status: string): StatusTone {
  if (status === "healthy") return "ok"
  if (status === "degraded" || status === "unknown") return "warn"
  if (status === "unhealthy") return "fail"
  return "neutral"
}

function toneForFeatureStatus(status: string): StatusTone {
  if (status === "ok") return "ok"
  if (status === "degraded") return "warn"
  if (status === "unavailable") return "fail"
  return "neutral"
}

const seededRuns: ConsoleRow[] = [
  {
    id: "run_8d92f0",
    primary: "summariser.run",
    secondary: "tenant acme · gpt-4o-mini · HTTP",
    status: "succeeded",
    tone: "ok",
    metric: "$0.04 · 0.9 s",
    timestamp: "09:14:22",
  },
  {
    id: "run_117b2e",
    primary: "coder.review",
    secondary: "tenant beta · claude-sonnet · webhook",
    status: "failed",
    tone: "fail",
    metric: "$0.37 · 3.4 s",
    timestamp: "09:14:08",
  },
  {
    id: "run_40caa1",
    primary: "support.triage",
    secondary: "tenant acme · gpt-4.1-mini · job",
    status: "running",
    tone: "running",
    metric: "$0.12 · 12 s",
    timestamp: "09:13:51",
  },
  {
    id: "run_20f7cd",
    primary: "analyst.synth",
    secondary: "tenant delta · gemini-flash · cron",
    status: "succeeded",
    tone: "ok",
    metric: "$0.18 · 2.1 s",
    timestamp: "09:13:12",
  },
]

const seededTenants: ConsoleRow[] = [
  {
    id: "tenant_acme",
    primary: "acme",
    secondary: "3 members · 2 active keys · budget $500",
    status: "healthy",
    tone: "ok",
    metric: "$184.20 MTD",
    timestamp: "created 12d ago",
  },
  {
    id: "tenant_beta",
    primary: "beta-labs",
    secondary: "8 members · 4 active keys · budget $1,200",
    status: "near budget",
    tone: "warn",
    metric: "$998.10 MTD",
    timestamp: "created 31d ago",
  },
  {
    id: "tenant_delta",
    primary: "delta-health",
    secondary: "2 members · 1 active key · budget $300",
    status: "healthy",
    tone: "ok",
    metric: "$77.82 MTD",
    timestamp: "created 6d ago",
  },
]

const seededServices: ServiceVital[] = [
  { name: "postgres", version: "16.2", status: "healthy", checked: "13s ago", adapter: "managed" },
  { name: "litellm", version: "v1.48", status: "healthy", checked: "13s ago", adapter: "LiteLLM" },
  { name: "agentfield", version: "v1.8.0", status: "healthy", checked: "13s ago", adapter: "AgentField" },
  { name: "river", version: "v2.3.1", status: "healthy", checked: "13s ago", adapter: "River" },
  { name: "svix", version: "local", status: "degraded", checked: "41s ago", adapter: "Svix" },
  { name: "minio", version: "2026.05", status: "healthy", checked: "13s ago", adapter: "S3" },
]

const seededAdapters: AdapterSlot[] = [
  { slot: "LLM gateway", adapter: "LiteLLM", status: "ok", description: "Model routing, spend recording, and cache stats." },
  { slot: "Agent runtime", adapter: "AgentField", status: "ok", description: "Execution graphs, runs, spans, memory, and approvals." },
  { slot: "Database", adapter: "Postgres", status: "ok", description: "Tenant isolation, RLS, audit, and app records." },
  { slot: "Object storage", adapter: "S3 / MinIO", status: "ok", description: "Tenant-prefixed blobs and signed URLs." },
  { slot: "Job queue", adapter: "River", status: "running", description: "Async jobs, retries, dead letters, and cron dispatch." },
  { slot: "Webhooks out", adapter: "Svix", status: "warn", description: "Delivery log, signing, replay, and endpoint config." },
  { slot: "Auth", adapter: "Better Auth", status: "ok", description: "Operator session and customer-app signups." },
  { slot: "Billing", adapter: "Lago / Stripe", status: "neutral", description: "Customer records, meters, portals, and invoices." },
  { slot: "Sandbox", adapter: "E2B / local", status: "running", description: "Code execution, artifacts, network policy, and cost." },
  { slot: "Notifications", adapter: "Log / Resend", status: "neutral", description: "Email, SMS, push, and delivery outbox." },
]

export const seededSnapshot: OperatorSnapshot = {
  source: "seeded",
  generatedAt: nowIso(),
  runtimeStatus: "seeded",
  endpointStatus: {
    logs: "missing",
    traces: "degraded",
    adapters: "degraded",
    auth: "degraded",
    llm: "degraded",
    deployTargets: "missing",
    brand: "missing",
  },
  kpis: [
    { label: "Requests / min", value: "1,240", detail: "last 5 min", trend: "+4%", tone: "ok", sparkline: [32, 29, 27, 28, 30, 22, 21, 24] },
    { label: "Error rate · 1h", value: "2.1%", detail: "41 failed events", trend: "+0.4 pts", tone: "warn", sparkline: [8, 6, 7, 6, 10, 8, 7, 7] },
    { label: "Cost today", value: "$84.20", detail: "all tenants", trend: "+12%", tone: "neutral", sparkline: [8, 12, 12, 19, 21, 29, 31, 34] },
    { label: "Cost month-to-date", value: "$1,902", detail: "Jun 1-15", trend: "61% cap", tone: "warn", sparkline: [4, 9, 14, 16, 23, 24, 29, 31] },
    { label: "Queue depth", value: "17", detail: "3 retrying", trend: "-9", tone: "running", sparkline: [18, 17, 16, 15, 19, 17, 16, 15] },
    { label: "Running now", value: "6", detail: "4 agents", trend: "+2", tone: "running", sparkline: [2, 3, 2, 4, 3, 3, 2, 4] },
    { label: "Failed · 24h", value: "3", detail: "2 tenants", trend: "-2", tone: "ok", sparkline: [7, 5, 4, 5, 5, 4, 3, 3] },
    { label: "Budget used", value: "61%", detail: "all tenants", trend: "safe", tone: "warn", sparkline: [42, 45, 49, 50, 55, 58, 60, 61] },
  ],
  activity: [
    ...seededRuns,
    { id: "audit_91d", primary: "budget.threshold_crossed", secondary: "beta-labs crossed 80% of monthly cap", status: "warning", tone: "warn", metric: "audit", timestamp: "09:10:12" },
    { id: "tenant_201", primary: "tenant.created", secondary: "new tenant delta-health from customer-app signup", status: "healthy", tone: "ok", metric: "customers", timestamp: "08:54:41" },
  ],
  runs: seededRuns,
  costRows: [
    { id: "cost_1", primary: "beta-labs", secondary: "claude-sonnet · coder.review", status: "top tenant", tone: "warn", metric: "$998.10", timestamp: "30d" },
    { id: "cost_2", primary: "acme", secondary: "gpt-4o-mini · support.triage", status: "stable", tone: "ok", metric: "$184.20", timestamp: "30d" },
    { id: "cost_3", primary: "delta-health", secondary: "gemini-flash · analyst.synth", status: "stable", tone: "ok", metric: "$77.82", timestamp: "30d" },
  ],
  tenants: seededTenants,
  apiKeys: [
    { id: "key_prod", primary: "prod-server", secondary: "tenant acme · rpm 120 · tpm 80k", status: "active", tone: "ok", metric: "$44.80", timestamp: "rotates in 18d" },
    { id: "key_mobile", primary: "mobile-preview", secondary: "tenant beta-labs · rpm 40 · tpm 20k", status: "active", tone: "ok", metric: "$19.10", timestamp: "rotates in 4d" },
  ],
  members: [
    { id: "usr_1", primary: "sara@acme.test", secondary: "acme · owner", status: "active", tone: "ok", metric: "last seen 4m", timestamp: "joined 12d ago" },
    { id: "usr_2", primary: "ops@beta.test", secondary: "beta-labs · admin", status: "active", tone: "ok", metric: "last seen 1h", timestamp: "joined 31d ago" },
  ],
  agents: [
    { id: "support.triage", primary: "support.triage", secondary: "reasoners: classify, route, answer", status: "healthy", tone: "ok", metric: "342 runs", timestamp: "v0.9.2" },
    { id: "coder.review", primary: "coder.review", secondary: "reasoners: diff, risk, comment", status: "degraded", tone: "warn", metric: "88 runs", timestamp: "v0.7.1" },
    { id: "summariser.run", primary: "summariser.run", secondary: "reasoners: ingest, compress", status: "healthy", tone: "ok", metric: "1,204 runs", timestamp: "v1.2.0" },
  ],
  tables: [
    { id: "suite_tenants", primary: "suite_tenants", secondary: "tenant records and isolation metadata", status: "rls on", tone: "ok", metric: "3 rows", timestamp: "updated 9m" },
    { id: "suite_runs", primary: "suite_runs", secondary: "runtime execution summaries", status: "rls on", tone: "ok", metric: "1,642 rows", timestamp: "updated now" },
    { id: "suite_cost_events", primary: "suite_cost_events", secondary: "provider spend and token ledger", status: "rls on", tone: "ok", metric: "8,921 rows", timestamp: "updated now" },
  ],
  dbHealth: [
    { id: "db-connections", primary: "Connections", secondary: "Postgres connection pool", status: "healthy", tone: "ok", metric: "0 active", timestamp: "current" },
    { id: "db-cache", primary: "Cache hit ratio", secondary: "pg_statio_user_tables", status: "healthy", tone: "ok", metric: "n/a", timestamp: "current" },
  ],
  queue: [
    { id: "job_1", primary: "billing.syncCustomer", secondary: "tenant beta-labs", status: "retrying", tone: "warn", metric: "attempt 2/5", timestamp: "09:12:02" },
    { id: "job_2", primary: "webhook.deliver", secondary: "endpoint whsec_live", status: "running", tone: "running", metric: "240 ms", timestamp: "09:14:10" },
  ],
  errors: [
    { id: "err_1", primary: "provider_rate_limit", secondary: "coder.review · beta-labs · 14 occurrences", status: "active", tone: "fail", metric: "last 2m", timestamp: "09:13:58" },
    { id: "err_2", primary: "webhook_5xx", secondary: "customer endpoint returned 503", status: "muted", tone: "warn", metric: "4 hits", timestamp: "08:44:10" },
  ],
  traces: [
    { id: "trace_88af", primary: "POST /api/v1/agents/support.triage", secondary: "12 spans · critical path llm.chat", status: "succeeded", tone: "ok", metric: "1.7 s", timestamp: "09:14:22" },
    { id: "trace_21ca", primary: "webhook stripe.invoice.created", secondary: "18 spans · job enqueue slow", status: "warning", tone: "warn", metric: "4.9 s", timestamp: "09:10:51" },
  ],
  logs: [
    { id: "log_1", primary: "provider rate limit", secondary: "runtime · run_id=run_117b2e tenant=beta-labs", status: "error", tone: "fail", metric: "llm.gateway", timestamp: "09:13:58" },
    { id: "log_2", primary: "webhook delivery retry scheduled", secondary: "worker · delivery=wh_2 attempt=2", status: "warn", tone: "warn", metric: "webhooks", timestamp: "09:09:44" },
    { id: "log_3", primary: "agent run completed", secondary: "runtime · run_id=run_8d92f0", status: "info", tone: "ok", metric: "runs", timestamp: "09:14:22" },
  ],
  cache: [
    { id: "cache_hits", primary: "Cache hits", secondary: "LLM cache calls served without provider spend", status: "healthy", tone: "ok", metric: "68%", timestamp: "24h" },
    { id: "cache_misses", primary: "Top misses", secondary: "Prompts not reusable in current policy window", status: "watch", tone: "warn", metric: "$12.40", timestamp: "24h" },
    { id: "cache_entries", primary: "Entries", secondary: "Stored prompt and response fingerprints", status: "active", tone: "running", metric: "1,284", timestamp: "current" },
  ],
  webhooks: [
    { id: "wh_1", primary: "invoice.created", secondary: "outbound · https://api.acme.test/webhooks", status: "delivered", tone: "ok", metric: "202", timestamp: "09:11:02" },
    { id: "wh_2", primary: "run.failed", secondary: "outbound · https://hooks.beta.test/backai", status: "failed", tone: "fail", metric: "503", timestamp: "09:09:42" },
  ],
  webhookEndpoints: [
    { id: "whend_github", primary: "github-pr", secondary: "github · forwards to af://agents/coder.review", status: "active", tone: "ok", metric: "sha256", timestamp: "current" },
    { id: "whend_stripe", primary: "stripe-billing", secondary: "stripe · forwards to billing.syncCustomer", status: "active", tone: "ok", metric: "sha256", timestamp: "current" },
  ],
  approvals: [
    { id: "appr_release", primary: "Release deployment", secondary: "coder.review requested approval for tenant beta-labs", status: "pending", tone: "running", metric: "waiting 12m", timestamp: "09:02:12" },
    { id: "appr_budget", primary: "Budget override", secondary: "support.triage requested cap increase", status: "approved", tone: "ok", metric: "8m decision", timestamp: "08:42:33" },
  ],
  audit: [
    { id: "aud_1", primary: "admin.keys.issue", secondary: "prod-server issued for tenant acme", status: "recorded", tone: "ok", metric: "operator", timestamp: "08:40:09" },
    { id: "aud_2", primary: "budgets.set", secondary: "beta-labs cap set to $1,200", status: "recorded", tone: "ok", metric: "operator", timestamp: "07:12:18" },
  ],
  oauth: [
    { id: "oauth_google", primary: "Google", secondary: "tenant acme · user sara@acme.test · drive.readonly", status: "active", tone: "ok", metric: "expires 21d", timestamp: "connected" },
    { id: "oauth_slack", primary: "Slack", secondary: "tenant beta-labs · user ops@beta.test · chat:write", status: "refresh failed", tone: "warn", metric: "needs action", timestamp: "09:01:44" },
  ],
  sessions: [
    { id: "sess_1", primary: "sara@acme.test", secondary: "acme · Chrome macOS · MFA yes", status: "active", tone: "ok", metric: "last 4m", timestamp: "expires 2h" },
    { id: "sess_2", primary: "ops@beta.test", secondary: "beta-labs · Safari iOS · MFA no", status: "active", tone: "running", metric: "last 1h", timestamp: "expires 3h" },
  ],
  jobs: [
    { id: "job_billing", primary: "billing.syncCustomer", secondary: "tenant beta-labs · last error: provider timeout", status: "retrying", tone: "warn", metric: "attempt 2/5", timestamp: "09:12:02" },
    { id: "job_webhook", primary: "webhook.deliver", secondary: "endpoint whsec_live · no error", status: "running", tone: "running", metric: "240 ms", timestamp: "09:14:10" },
  ],
  jobDefinitions: [
    { id: "jobdef_webhook", primary: "webhook.deliver", secondary: "outbound delivery worker with retry policy", status: "enabled", tone: "ok", metric: "5 attempts", timestamp: "registered" },
    { id: "jobdef_billing", primary: "billing.syncCustomer", secondary: "billing adapter sync and meter push", status: "enabled", tone: "ok", metric: "cron", timestamp: "registered" },
  ],
  storage: [
    { id: "obj_acme_transcripts", primary: "acme/transcripts", secondary: "run artifacts and source uploads", status: "active", tone: "ok", metric: "8.2 GB", timestamp: "updated 5m" },
    { id: "obj_platform_cache", primary: "platform/cache", secondary: "compiled artifacts and prompt cache exports", status: "active", tone: "running", metric: "2.1 GB", timestamp: "updated 21m" },
  ],
  memory: [
    { id: "mem_acme_support", primary: "tenant/acme/support.triage", secondary: "customer support summaries and preferences", status: "embedded", tone: "ok", metric: "840 entries", timestamp: "updated 2m" },
    { id: "mem_platform_docs", primary: "global/platform/docs", secondary: "shared API and product context", status: "embedded", tone: "ok", metric: "1,219 entries", timestamp: "updated 1h" },
  ],
  searchIndexes: [
    { id: "idx_platform_docs", primary: "platform-docs", secondary: "API, SDK, and internal product documentation", status: "healthy", tone: "ok", metric: "7.9k chunks", timestamp: "fresh" },
    { id: "idx_acme_support", primary: "acme-support", secondary: "tickets, knowledge base, and resolved answers", status: "healthy", tone: "ok", metric: "3.1k chunks", timestamp: "fresh" },
  ],
  modules: [
    { id: "mod_admin", primary: "admin", secondary: "tenants, API keys, users, audit, budgets", status: "mounted", tone: "ok", metric: "9 routes", timestamp: "current" },
    { id: "mod_data", primary: "data", secondary: "tables, SQL, storage, memory, search", status: "mounted", tone: "ok", metric: "8 routes", timestamp: "current" },
  ],
  skills: [
    { id: "skill_shadcn", primary: "shadcn/ui", secondary: "component registry, source, and demos", status: "connected", tone: "ok", metric: "56 components", timestamp: "probed now" },
    { id: "skill_browser", primary: "Browser", secondary: "local app navigation and screenshots", status: "connected", tone: "ok", metric: "automation", timestamp: "probed now" },
  ],
  tools: [
    { id: "tool_browser", primary: "browser", secondary: "browser-use adapter · navigate, click, extract", status: "enabled", tone: "ok", metric: "3 verbs", timestamp: "current" },
    { id: "tool_search", primary: "search", secondary: "searxng adapter · query and retrieve", status: "enabled", tone: "ok", metric: "2 verbs", timestamp: "current" },
    { id: "tool_exec", primary: "exec", secondary: "local exec adapter · command sandboxed by policy", status: "configured", tone: "running", metric: "4 verbs", timestamp: "current" },
  ],
  reasoners: [
    { id: "support.triage/classify", primary: "support.triage/classify", secondary: "entry reasoner · routes support requests", status: "declared", tone: "ok", metric: "schema", timestamp: "runtime" },
    { id: "coder.review/risk", primary: "coder.review/risk", secondary: "analysis reasoner · tool-enabled", status: "declared", tone: "ok", metric: "3 tools", timestamp: "runtime" },
  ],
  harnesses: [
    { id: "claude-code", primary: "Claude Code", secondary: "binary resolved · auth present", status: "ready", tone: "ok", metric: "probed", timestamp: "current" },
    { id: "codex", primary: "Codex", secondary: "binary resolved · model route configured", status: "ready", tone: "ok", metric: "probed", timestamp: "current" },
  ],
  crons: [
    { id: "cron_billing", primary: "billing.syncCustomer", secondary: "0 */6 * * * · sync billing adapters", status: "active", tone: "running", metric: "next 11:00", timestamp: "last ok" },
    { id: "cron_cleanup", primary: "storage.cleanupArtifacts", secondary: "0 3 * * * · remove expired artifacts", status: "active", tone: "running", metric: "next 03:00", timestamp: "last ok" },
  ],
  featureFlags: [
    { id: "flag_new_admin", primary: "new-admin-console", secondary: "routes / to the new operator console", status: "enabled", tone: "ok", metric: "100%", timestamp: "current" },
    { id: "flag_stream_runs", primary: "stream-runs", secondary: "server-sent event updates for run lists", status: "enabled", tone: "ok", metric: "100%", timestamp: "current" },
  ],
  billing: [
    { id: "bill_acme", primary: "acme", secondary: "Stripe customer cus_acme · usage meter active", status: "synced", tone: "ok", metric: "$184.20", timestamp: "4m ago" },
    { id: "bill_beta", primary: "beta-labs", secondary: "Lago customer lag_102 · budget warning active", status: "synced", tone: "warn", metric: "$998.10", timestamp: "7m ago" },
  ],
  secrets: [
    { id: "sec_openai", primary: "OPENAI_API_KEY", secondary: "LLM provider route", status: "active", tone: "ok", metric: "rotates 22d", timestamp: "access 3m" },
    { id: "sec_svix", primary: "SVIX_TOKEN", secondary: "webhook delivery adapter", status: "rotation due", tone: "warn", metric: "rotates 4d", timestamp: "access 9m" },
  ],
  notifications: [
    { id: "notif_budget", primary: "budget-warning", secondary: "email · ops@beta.test · resend", status: "sent", tone: "ok", metric: "attempt 1", timestamp: "08:44:10" },
    { id: "notif_invite", primary: "tenant-invite", secondary: "log · sara@acme.test", status: "queued", tone: "running", metric: "attempt 0", timestamp: "09:12:01" },
  ],
  notificationMutes: [
    { id: "mute-default", primary: "*", secondary: "* · * · *", status: "available", tone: "neutral", metric: "no active mute", timestamp: "policy" },
  ],
  llmProviders: [
    { id: "llm_openai", primary: "OpenAI", secondary: "gpt-4.1, gpt-4o-mini, embeddings", status: "healthy", tone: "ok", metric: "8 models", timestamp: "current" },
    { id: "llm_anthropic", primary: "Anthropic", secondary: "claude-sonnet and fallback review models", status: "healthy", tone: "ok", metric: "5 models", timestamp: "current" },
  ],
  providerHealth: [
    { id: "provider-health-litellm", primary: "litellm", secondary: "provider poller", status: "pending", tone: "running", metric: "0 observations", timestamp: "waiting" },
  ],
  sandbox: [
    { id: "sandbox_pool", primary: "Sandbox pool", secondary: "remote isolated execution and artifact upload", status: "available", tone: "running", metric: "6 slots", timestamp: "current" },
    { id: "sandbox_policy", primary: "Network policy", secondary: "default deny with provider allow-list", status: "restricted", tone: "running", metric: "safe", timestamp: "current" },
  ],
  observability: [
    { id: "obs_metrics", primary: "Metrics", secondary: "runtime, heap, latency, and route rollups", status: "enabled", tone: "ok", metric: "scrape", timestamp: "current" },
    { id: "obs_logs", primary: "Logs", secondary: "process-local recent runtime log ring", status: "enabled", tone: "ok", metric: "tail", timestamp: "current" },
  ],
  deployTargets: [
    { id: "deploy_runtime", primary: "Runtime API", secondary: "agent and admin API target", status: "ready", tone: "ok", metric: "8080", timestamp: "checked 2m" },
    { id: "deploy_dashboard", primary: "Dashboard", secondary: "Next.js admin app and old admin fallback", status: "ready", tone: "ok", metric: "33000", timestamp: "checked 2m" },
  ],
  services: seededServices,
  brand: [
    { id: "brand-source", primary: "Source", secondary: "brand.yaml plus DB override", status: "file", tone: "neutral", metric: "brand.yaml", timestamp: "current" },
    { id: "brand-restart", primary: "Apply behavior", secondary: "Customer-app build-time surfaces need restart/redeploy", status: "restart", tone: "warn", metric: "required", timestamp: "policy" },
  ],
  adapters: seededAdapters,
  logCapabilities: {
    supports_tail: true,
    supports_full_text: true,
    supports_regex_search: false,
    supports_trace_id: false,
    native_query_lang: "",
    retention_days: 0,
    max_entries_per_page: 1000,
  },
  traceCapabilities: {
    supports_traceql: false,
    supports_tag_search: false,
    native_query_lang: "",
    retention_hours: 0,
    max_results_per_query: 0,
  },
  metricsCapabilities: {
    supports_instant_query: false,
    supports_range_query: false,
    supports_container_metrics: false,
    native_query_lang: "",
    retention_hours: 0,
    max_series_per_query: 0,
  },
  costSeries: [
    { id: "cost-series-none", primary: "Metrics backend", secondary: "configure a metrics backend to see time-series charts", status: "not configured", tone: "neutral", metric: "none", timestamp: "adapter" },
  ],
  containerMetrics: [
    { id: "container-metrics-none", primary: "Container metrics", secondary: "configure Prometheus with cAdvisor to inspect containers", status: "not configured", tone: "neutral", metric: "none", timestamp: "adapter" },
  ],
  features: [
    { id: "feature-db-health", primary: "db_health", secondary: "Database health surface", status: "ok", tone: "ok", metric: "enabled", timestamp: "preset" },
    { id: "feature-logs", primary: "logs", secondary: "Future logs adapter slot", status: "not_configured", tone: "neutral", metric: "ring", timestamp: "preset" },
  ],
  featureWarnings: [],
  budgets: [
    { tenant: "acme", cap: "$500", used: 37, status: "ok" },
    { tenant: "beta-labs", cap: "$1,200", used: 83, status: "warn" },
    { tenant: "delta-health", cap: "$300", used: 26, status: "ok" },
  ],
  snippets: {
    runtimeUrl: "http://localhost:8080",
    tenantKey: "af_live_demo_••••_2x91",
    curl: "curl -H 'Authorization: Bearer $BACKAI_KEY' http://localhost:8080/api/v1/agents",
    typescript: "const runs = await suite.runs.list({ limit: 10 })",
    python: "runs = client.runs.list(limit=10)",
    go: "runs, err := client.Runs.List(ctx, afstack.RunsListParams{Limit: 10})",
  },
}

export async function getOperatorSnapshot(): Promise<OperatorSnapshot> {
  const [
    health,
    home,
    cost,
    runs,
    queue,
    agents,
    tenants,
    keys,
    users,
    audit,
    budgets,
    tables,
    dbHealth,
    webhooks,
    webhookEndpoints,
    models,
    cacheStats,
    providerHealth,
    costEvents,
    jobs,
    jobDefinitions,
    storage,
    memory,
    modules,
    skills,
    flags,
    billingCustomers,
    billingMeters,
    secrets,
    notificationStats,
    notifications,
    notificationMutes,
    sandboxPool,
    sandboxRuns,
    metrics,
    metricsCapabilities,
    costMetricRange,
    containerCpuRange,
    containerMemory,
    containerRestarts,
    toolAdapters,
    nativeTools,
    mcpServers,
    mcpTools,
    harnesses,
    crons,
    plugins,
    adapterRegistry,
    connectedServices,
    searchIndexStats,
    brand,
    approvals,
    logs,
    logCapabilities,
    traces,
    traceCapabilities,
    oauthConnections,
    oauthProviders,
    features,
  ] = await Promise.all([
    settle(() => api.health()),
    settle(() => api.home()),
    settle(() => api.cost()),
    settle(() => api.runs({ limit: 16 })),
    settle(() => api.queue()),
    settle(() => api.agents()),
    settle(() => api.admin.tenants.list()),
    settle(() => api.admin.keys.list()),
    settle(() => api.admin.users.list()),
    settle(() => api.admin.audit.list({ limit: 20 })),
    settle(() => api.budgets.list()),
    settle(() => api.db.tables()),
    settle(() => api.db.health()),
    settle(() => api.webhooks.deliveries({ limit: 10 })),
    settle(() => api.webhooks.endpoints()),
    settle(() => api.llm.models()),
    settle(() => api.llm.cacheStats()),
    settle(() => api.admin.llm.providerHealth({ window: "24h" })),
    settle(() => api.costEvents({ limit: 20 })),
    settle(() => api.jobs.list({ limit: 12 })),
    settle(() => api.jobs.definitions()),
    settle(() => api.storage.list({ limit: 20 })),
    settle(() => api.memory.list({ limit: 20 })),
    settle(() => api.modulesState()),
    settle(() => api.skills.list()),
    settle(() => api.config.flags.list()),
    settle(() => api.billing.customers()),
    settle(() => api.billing.meters()),
    settle(() => api.secrets.list()),
    settle(() => api.notifications.stats()),
    settle(() => api.notifications.list({ limit: 20 })),
    settle(() => api.notifications.mutes.list()),
    settle(() => api.sandbox.pool()),
    settle(() => api.sandbox.list({ limit: 12 })),
    settle(() => api.metrics()),
    settle(() => api.metrics.capabilities()),
    settle(() => api.metrics.range({
      promql: "increase(backai_cost_usd_total[1h])",
      from: new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString(),
      to: new Date().toISOString(),
      step: "1h",
    })),
    settle(() => api.metrics.range({
      promql: 'rate(container_cpu_usage_seconds_total{name=~"backai-.*"}[5m])',
      from: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
      to: new Date().toISOString(),
      step: "5m",
    })),
    settle(() => api.metrics.query({ promql: 'container_memory_usage_bytes{name=~"backai-.*"}' })),
    settle(() => api.metrics.range({
      promql: 'changes(container_start_time_seconds{name=~"backai-.*"}[24h])',
      from: new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString(),
      to: new Date().toISOString(),
      step: "1h",
    })),
    settle(() => api.tools.adapters()),
    settle(() => api.tools.listNative()),
    settle(() => api.mcp.servers()),
    settle(() => api.mcp.tools()),
    settle(() => api.harnesses.list()),
    settle(() => api.crons.list()),
    settle(() => api.plugins()),
    settle(() => api.admin.adapters.list()),
    settle(() => api.admin.services.list()),
    settle(() => api.search.indexes()),
    settle(() => api.admin.brand.get()),
    settle(() => api.approvals.list({ limit: 20 })),
    settle(() => api.logs({ limit: 120 })),
    settle(() => api.logsCapabilities()),
    settle(() => api.traces.list({ limit: 20 })),
    settle(() => api.traces.capabilities()),
    settle(() => api.oauth.connections()),
    settle(() => api.oauth.providers()),
    settle(() => api.admin.features.get()),
  ])

  const live = Boolean(health || home || cost || runs || queue || agents || tenants)
  if (!live) return { ...seededSnapshot, generatedAt: nowIso() }

  const runRows: ConsoleRow[] =
    runs?.runs.map((run) => ({
      id: run.id,
      primary: run.agent,
      secondary: `${run.tenant_id ?? "platform"} · trigger unknown`,
      status: run.status,
      tone: toneForStatus(run.status),
      metric: `${money(run.cost_usd)} · ${duration(run.duration_ms)}`,
      timestamp: shortTime(run.started_at),
    })) ?? seededSnapshot.runs

  const tenantRows: ConsoleRow[] =
    tenants?.tenants.map((tenant) => ({
      id: tenant.id,
      primary: tenant.name,
      secondary: `${tenant.slug} · ${tenant.plan} plan`,
      status: tenant.deleted_at ? "deleted" : "active",
      tone: tenant.deleted_at ? "fail" : "ok",
      metric: tenant.created_at ? shortTime(tenant.created_at) : "tenant",
      timestamp: tenant.deleted_at ? `deleted ${shortTime(tenant.deleted_at)}` : "current",
    })) ?? seededSnapshot.tenants

  const costRows: ConsoleRow[] =
    cost?.by_tenant.map((item) => ({
      id: item.tenant_id,
      primary: item.tenant_name ?? item.tenant_id,
      secondary: "period spend by tenant",
      status: "active",
      tone: "ok",
      metric: money(item.cost_usd),
      timestamp: "selected period",
    })) ?? seededSnapshot.costRows

  const costEventRows: ConsoleRow[] =
    costEvents?.events.map((event) => ({
      id: event.id,
      primary: event.model,
      secondary: `${event.tenant_id ?? "platform"} · ${event.provider}${event.agent ? ` · ${event.agent}` : ""}`,
      status: event.cached ? "cache hit" : "metered",
      tone: event.cached ? "ok" : "running",
      metric: `${money(event.cost_usd)} · ${event.total_tokens.toLocaleString("en")} tok`,
      timestamp: shortTime(event.occurred_at),
    })) ?? costRows

  const costSeriesRows = metricRangeRows(costMetricRange, "cost")
  const containerMetricRows = [
    ...metricRangeRows(containerCpuRange, "cpu"),
    ...metricInstantRows(containerMemory, "memory"),
    ...metricRangeRows(containerRestarts, "restart"),
  ]
  const costSparkline = costMetricRange?.series?.[0]?.values.map((value) => value.value)

  const jobRows: ConsoleRow[] =
    jobs?.jobs.map((job) => ({
      id: job.id,
      primary: job.name,
      secondary: job.errors?.[0]?.error ?? "no error",
      status: job.state,
      tone: toneForStatus(job.state),
      metric: `${job.attempt}/${job.max_attempts} attempts`,
      timestamp: shortTime(job.enqueued_at),
    })) ?? seededSnapshot.jobs

  const jobDefinitionRows: ConsoleRow[] =
    jobDefinitions?.definitions.map((definition) => ({
      id: definition.name,
      primary: definition.name,
      secondary: definition.description ?? definition.source_path ?? "registered workload job",
      status: definition.recent.failed > 0 ? "warning" : "enabled",
      tone: definition.recent.failed > 0 ? "warn" : "ok",
      metric: `${definition.recent.running} running`,
      timestamp: definition.cron ?? definition.language,
    })) ?? seededSnapshot.jobDefinitions

  const storageRows: ConsoleRow[] =
    storage?.objects.map((object) => ({
      id: object.key,
      primary: object.key,
      secondary: `${object.content_type ?? "object"} · ${object.etag ?? "no etag"}`,
      status: "stored",
      tone: "ok",
      metric: `${Math.max(1, Math.round(object.size / 1024)).toLocaleString("en")} KiB`,
      timestamp: shortTime(object.last_modified),
    })) ?? seededSnapshot.storage

  const memoryRows: ConsoleRow[] =
    memory?.entries.map((entry) => ({
      id: `${entry.scope}:${entry.scope_id ?? "platform"}:${entry.key}`,
      primary: `${entry.scope}/${entry.scope_id ?? "platform"}`,
      secondary: entry.key,
      status: entry.has_embedding ? "embedded" : "stored",
      tone: entry.has_embedding ? "ok" : "running",
      metric: Object.keys(entry.metadata ?? {}).length ? `${Object.keys(entry.metadata).length} meta` : "memory",
      timestamp: shortTime(entry.updated_at),
    })) ?? seededSnapshot.memory

  const moduleRows: ConsoleRow[] =
    modules?.modules.map((module) => ({
      id: module.id,
      primary: module.name,
      secondary: `${module.adapter ?? "native"}${module.version ? ` · ${module.version}` : ""}`,
      status: module.enabled ? "mounted" : "disabled",
      tone: module.enabled ? "ok" : "neutral",
      metric: modules.workload_modules.includes(module.id) ? "workload" : "system",
      timestamp: "runtime",
    })) ?? seededSnapshot.modules

  const skillRows: ConsoleRow[] = [
    ...(mcpServers?.servers.map((server) => ({
      id: `mcp:${server.name}`,
      primary: server.name,
      secondary: `${server.transport} · ${server.description}`,
      status: server.status,
      tone: toneForStatus(server.status),
      metric: `${server.tools_count} tools`,
      timestamp: server.last_connected_at ? shortTime(server.last_connected_at) : "not connected",
    })) ?? []),
    ...(skills?.skills.map((skill) => ({
      id: skill.id,
      primary: skill.name,
      secondary: `${skill.version} · ${skill.harnesses.join(", ") || "any harness"}`,
      status: "installed",
      tone: "ok" as StatusTone,
      metric: skill.tags.slice(0, 2).join(", ") || "skill",
      timestamp: shortTime(skill.installed_at),
    })) ?? []),
    ...(mcpTools?.tools.slice(0, 8).map((tool) => ({
      id: `mcp-tool:${tool.id}`,
      primary: tool.name,
      secondary: `${tool.server} · ${tool.description ?? "tool schema available"}`,
      status: "available",
      tone: "running" as StatusTone,
      metric: Object.keys(tool.input_schema ?? {}).length ? "schema" : "no schema",
      timestamp: "current",
    })) ?? []),
  ]

  const flagRows: ConsoleRow[] =
    flags?.flags.map((flag) => ({
      id: flag.key,
      primary: flag.label,
      secondary: flag.description,
      status: flag.enabled ? "enabled" : "disabled",
      tone: flag.enabled ? "ok" : "neutral",
      metric: flag.source,
      timestamp: flag.updated_at ? shortTime(flag.updated_at) : "default",
    })) ?? seededSnapshot.featureFlags

  const billingRows: ConsoleRow[] =
    billingCustomers?.customers.map((customer) => ({
      id: customer.tenant_id,
      primary: customer.tenant_id,
      secondary: `${billingCustomers.adapter} · ${customer.stripe_customer_id ?? "no provider id"} · ${customer.plan}`,
      status: customer.subscription_status ?? "free",
      tone: toneForStatus(customer.subscription_status ?? "ok"),
      metric: billingMeters?.meters.find((meter) => meter.tenant_id === customer.tenant_id)?.cost_usd == null
        ? "no metered cost"
        : money(billingMeters.meters.find((meter) => meter.tenant_id === customer.tenant_id)?.cost_usd),
      timestamp: shortTime(customer.updated_at),
    })) ?? seededSnapshot.billing

  const secretRows: ConsoleRow[] =
    secrets?.secrets.map((secret) => ({
      id: secret.key,
      primary: secret.key,
      secondary: `${secret.tenant_id ?? "platform"}${secret.description ? ` · ${secret.description}` : ""}`,
      status: secret.rotate_after ? "rotation policy" : "active",
      tone: "ok",
      metric: secret.rotate_after ? `rotates ${shortTime(secret.rotate_after)}` : "no rotation",
      timestamp: shortTime(secret.updated_at),
    })) ?? seededSnapshot.secrets

  const notificationRows: ConsoleRow[] =
    notifications?.notifications.length
      ? notifications.notifications.map((notification) => ({
          id: notification.id,
          primary: notification.template,
          secondary: `${notification.kind} · ${notification.to} · ${notification.adapter ?? "pending adapter"}`,
          status: notification.status,
          tone: toneForStatus(notification.status),
          metric: `${notification.attempts} attempts`,
          timestamp: shortTime(notification.created_at),
        }))
      : notificationStats
        ? [
            {
              id: "notifications-stats",
              primary: "Notification outbox",
              secondary: notificationStats.by_adapter.map((item) => `${item.adapter}:${item.count}`).join(" · ") || "no adapter volume",
              status: notificationStats.failed_today > 0 ? "failures" : "healthy",
              tone: notificationStats.failed_today > 0 ? "warn" : "ok",
              metric: `${notificationStats.sent_today} sent`,
              timestamp: `${notificationStats.failed_today} failed`,
            },
          ]
        : seededSnapshot.notifications

  const webhookEndpointRows: ConsoleRow[] =
    webhookEndpoints?.endpoints.map((endpoint) => ({
      id: endpoint.id,
      primary: endpoint.slug,
      secondary: `${endpoint.provider} · ${endpoint.forward_to}`,
      status: endpoint.is_active ? "active" : "disabled",
      tone: endpoint.is_active ? "ok" : "neutral",
      metric: endpoint.signature_algorithm ?? "unsigned",
      timestamp: shortTime(endpoint.created_at),
    })) ?? seededSnapshot.webhookEndpoints

  const llmChatSlot = adapterRegistry?.slots.find((slot) => slot.slot === "llm-chat")
  const llmChatCaps = llmChatSlot?.active.capabilities
  const llmVirtualKeysActive = llmChatCaps?.virtual_keys_active === true
  const llmKeyMode =
    typeof llmChatCaps?.key_management_mode === "string"
      ? llmChatCaps.key_management_mode
      : llmVirtualKeysActive
        ? "virtual_keys"
        : "stateless"
  const llmModeLabel = llmVirtualKeysActive ? "LiteLLM (virtual-keys)" : "LiteLLM (stateless)"
  const llmCapabilityRows: ConsoleRow[] = llmChatSlot
    ? [
        {
          id: "litellm-key-management",
          primary: llmModeLabel,
          secondary: `${llmChatSlot.active.name} · key management ${llmKeyMode}`,
          status: llmChatSlot.active.status,
          tone: toneForAdapterStatus(llmChatSlot.active.status),
          metric: llmVirtualKeysActive ? "mirrors keys" : "local only",
          timestamp: "runtime probe",
        },
      ]
    : []

  const modelRows: ConsoleRow[] =
    models?.models.map((model) => ({
      id: model.id,
      primary: model.display_name,
      secondary: `${model.provider} · ${model.supports_streaming ? "streaming" : "no streaming"} · ${model.supports_tools ? "tools" : "text"}`,
      status: "available",
      tone: "ok",
      metric: `${money(model.prompt_usd_per_1m)}/${money(model.completion_usd_per_1m)}`,
      timestamp: "current",
    })) ?? []
  const llmRows: ConsoleRow[] =
    llmCapabilityRows.length || modelRows.length
      ? [...llmCapabilityRows, ...modelRows]
      : seededSnapshot.llmProviders

  const sandboxRows: ConsoleRow[] =
    sandboxRuns?.runs.map((run) => ({
      id: run.id,
      primary: run.command.join(" "),
      secondary: `${run.tenant_id ?? "platform"} · ${run.adapter} · ${run.image}`,
      status: run.status,
      tone: toneForStatus(run.status),
      metric: run.duration_s ? duration(run.duration_s * 1000) : "pending",
      timestamp: shortTime(run.created_at),
    })) ??
    (sandboxPool
      ? [
          {
            id: "sandbox-pool",
            primary: "Sandbox pool",
            secondary: `${sandboxPool.adapter} · ${sandboxPool.active} active · ${sandboxPool.queued} queued`,
            status: sandboxPool.warm > 0 ? "available" : "full",
            tone: sandboxPool.warm > 0 ? "ok" : "warn",
            metric: `${sandboxPool.warm} warm`,
            timestamp: "current",
          },
        ]
      : seededSnapshot.sandbox)

  const approvalRows: ConsoleRow[] =
    approvals?.approvals.map((approval) => ({
      id: approval.id,
      primary: approval.kind,
      secondary: `${approval.tenant_id} · requested by ${approval.requested_by ?? "system"}`,
      status: approval.status,
      tone: toneForStatus(approval.status),
      metric: approval.decided_at ? `decided ${shortTime(approval.decided_at)}` : "awaiting decision",
      timestamp: shortTime(approval.created_at),
      href: `/operate/approvals?approval=${encodeURIComponent(approval.id)}`,
      detail: JSON.stringify(approval.payload, null, 2),
    })) ?? seededSnapshot.approvals

  const logRows: ConsoleRow[] =
    logs?.logs.map((line, index) => ({
      id: `${line.ts}:${index}`,
      primary: line.msg,
      secondary: `${line.service}${line.tenant_id ? ` · tenant=${line.tenant_id}` : ""}${line.request_id ? ` · request=${line.request_id}` : ""}`,
      status: line.level,
      tone: toneForStatus(line.level),
      metric: line.agent ?? line.service,
      timestamp: shortTime(line.ts),
      href: `/operate/logs?ts=${encodeURIComponent(line.ts)}`,
      detail: JSON.stringify(line, null, 2),
    })) ?? seededSnapshot.logs

  const firstTraceID = traces?.traces[0]?.trace_id
  const traceDetail: TraceDetail | null = firstTraceID
    ? await settle(() => api.traces.get(firstTraceID))
    : null

  const traceRows: ConsoleRow[] =
    traces?.traces.map((trace) => ({
      id: trace.trace_id,
      primary: trace.root_operation,
      secondary: `${trace.root_service} · ${trace.span_count} spans`,
      status: trace.status,
      tone: trace.status === "error" ? "fail" : trace.status === "ok" ? "ok" : "neutral",
      metric: `${trace.duration_ms.toLocaleString("en")} ms`,
      timestamp: trace.start_time ? shortTime(trace.start_time) : "unknown",
      href: `/operate/traces?trace=${encodeURIComponent(trace.trace_id)}`,
      detail: JSON.stringify(traceDetail?.trace_id === trace.trace_id ? traceDetail : trace, null, 2),
    })) ?? seededSnapshot.traces

  const oauthRows: ConsoleRow[] =
    oauthConnections?.connections.length
      ? oauthConnections.connections.map((connection) => ({
          id: connection.id ?? `${connection.provider}:${connection.account_id ?? connection.user_id ?? "unknown"}`,
          primary: connection.provider,
          secondary: `${connection.tenant_id ?? "platform"} · ${connection.user_id ?? connection.account_id ?? "connected account"}`,
          status: connection.status ?? "active",
          tone: toneForStatus(connection.status ?? "active"),
          metric: connection.expires_at ? `expires ${shortTime(connection.expires_at)}` : `${connection.scopes?.length ?? 0} scopes`,
          timestamp: connection.updated_at ? shortTime(connection.updated_at) : "current",
          href: `/customers/oauth?provider=${encodeURIComponent(connection.provider)}`,
          detail: connection.scopes?.join(", "),
        }))
      : oauthProviders?.providers.map((provider) => ({
          id: provider.provider,
          primary: provider.provider,
          secondary: provider.scopes?.join(", ") || "provider configured for OAuth flow",
          status: provider.configured ? "configured" : "missing",
          tone: provider.configured ? "ok" : "warn",
          metric: provider.auth_url ? "authorize" : "no auth URL",
          timestamp: "provider",
          href: `/customers/oauth?provider=${encodeURIComponent(provider.provider)}`,
        })) ?? seededSnapshot.oauth

  const sessionRows: ConsoleRow[] = [
    ...(users?.users.slice(0, 8).map((user) => ({
      id: `session:${user.id}`,
      primary: user.email,
      secondary: `${user.name ?? "customer user"} · active-session enumeration depends on auth adapter`,
      status: user.deleted_at ? "disabled" : "auth events only",
      tone: user.deleted_at ? "fail" : "warn" as StatusTone,
      metric: "capability gated",
      timestamp: user.created_at ? shortTime(user.created_at) : "unknown",
      href: `/customers/sessions?user=${encodeURIComponent(user.id)}`,
    })) ?? []),
  ]

  const toolRows: ConsoleRow[] = [
    ...(nativeTools?.tools.map((tool) => ({
      id: `native:${tool.tool}`,
      primary: tool.tool,
      secondary: `${tool.adapter_id} · ${tool.description}`,
      status: tool.enabled ? "enabled" : tool.configured ? "configured" : "missing",
      tone: tool.enabled ? "ok" : tool.configured ? "running" : "warn" as StatusTone,
      metric: `${tool.verbs.length} verbs`,
      timestamp: tool.updated_at ? shortTime(tool.updated_at) : "current",
      href: `/build/tools?tool=${encodeURIComponent(tool.tool)}`,
    })) ?? []),
    ...(toolAdapters?.adapters.map((adapter) => ({
      id: `adapter:${adapter.id}`,
      primary: adapter.label,
      secondary: adapter.description,
      status: adapter.enabled ? "enabled" : adapter.configured ? "configured" : "missing",
      tone: adapter.enabled ? "ok" : adapter.configured ? "running" : "warn" as StatusTone,
      metric: `${adapter.tools.length} tools`,
      timestamp: adapter.updated_at ? shortTime(adapter.updated_at) : "current",
      href: `/build/tools?adapter=${encodeURIComponent(adapter.id)}`,
    })) ?? []),
    ...(mcpTools?.tools.slice(0, 8).map((tool) => ({
      id: `mcp:${tool.id}`,
      primary: tool.name,
      secondary: `${tool.server} · ${tool.description ?? "MCP tool"}`,
      status: "available",
      tone: "running" as StatusTone,
      metric: "schema",
      timestamp: "current",
      href: `/build/tools?mcp=${encodeURIComponent(tool.id)}`,
    })) ?? []),
  ]

  const reasonerRows: ConsoleRow[] =
    agents?.agents.flatMap((agent) =>
      (agent.reasoners?.length ? agent.reasoners : ["default"]).map((reasoner) => ({
        id: `${agent.node_id}:${reasoner}`,
        primary: `${agent.node_id}/${reasoner}`,
        secondary: `${agent.version ?? "runtime"} · source agent ${agent.node_id}`,
        status: "declared",
        tone: "ok" as StatusTone,
        metric: agent.tags?.slice(0, 2).join(", ") || "schema",
        timestamp: "runtime",
        href: `/build/agents?agent=${encodeURIComponent(agent.node_id)}&reasoner=${encodeURIComponent(reasoner)}`,
      })),
    ) ?? seededSnapshot.reasoners

  const harnessRows: ConsoleRow[] =
    harnesses?.harnesses.map((harness) => ({
      id: harness.provider,
      primary: harness.provider,
      secondary: harness.binary_path ?? harness.last_error ?? "binary not resolved",
      status: harness.status,
      tone: toneForStatus(harness.status),
      metric: harness.version ?? (harness.required_env.join(", ") || "probe"),
      timestamp: "current",
      href: `/build/harnesses?provider=${encodeURIComponent(harness.provider)}`,
    })) ?? seededSnapshot.harnesses

  const cronRows: ConsoleRow[] =
    crons?.crons.map((cron) => ({
      id: cron.id,
      primary: cron.name,
      secondary: `${cron.job_name} · ${cron.schedule}`,
      status: cron.is_active ? "active" : "paused",
      tone: cron.is_active ? "running" : "neutral",
      metric: cron.next_run_at ? shortTime(cron.next_run_at) : "no next run",
      timestamp: cron.last_run_at ? `last ${shortTime(cron.last_run_at)}` : "never run",
      href: `/build/crons?cron=${encodeURIComponent(cron.id)}`,
    })) ?? seededSnapshot.crons

  const cacheRows: ConsoleRow[] =
    cacheStats
      ? [
          {
            id: "cache-hit-rate",
            primary: "Hit rate",
            secondary: `${cacheStats.cache_hits.toLocaleString("en")} hits from ${cacheStats.total_calls.toLocaleString("en")} total calls`,
            status: cacheStats.hit_rate >= 0.5 ? "healthy" : "watch",
            tone: cacheStats.hit_rate >= 0.5 ? "ok" : "warn",
            metric: `${Math.round(cacheStats.hit_rate * 100)}%`,
            timestamp: "selected range",
          },
          {
            id: "cache-savings",
            primary: "Estimated savings",
            secondary: "Provider spend avoided by cache hits",
            status: "derived",
            tone: "running",
            metric: money(cacheStats.savings_usd),
            timestamp: "client visible",
          },
          {
            id: "cache-entries",
            primary: "Entries",
            secondary: `${cacheStats.cache_misses.toLocaleString("en")} misses still went to provider`,
            status: "active",
            tone: "ok",
            metric: cacheStats.entries.toLocaleString("en"),
            timestamp: "current",
          },
        ]
      : seededSnapshot.cache

  const observabilityRows: ConsoleRow[] =
    metrics
      ? [
          {
            id: "http-p95",
            primary: "HTTP p95 latency",
            secondary: `${metrics.http_requests_total.toLocaleString("en")} requests since boot`,
            status: metrics.http_p95_ms > 1000 ? "slow" : "healthy",
            tone: metrics.http_p95_ms > 1000 ? "warn" : "ok",
            metric: duration(metrics.http_p95_ms),
            timestamp: `${Math.round(metrics.uptime_seconds / 60)}m uptime`,
          },
          {
            id: "runtime-memory",
            primary: "Runtime memory",
            secondary: `${metrics.goroutines.toLocaleString("en")} goroutines · ${metrics.version}`,
            status: "enabled",
            tone: "ok",
            metric: `${Math.round(metrics.heap_alloc_bytes / 1024 / 1024)} MiB`,
            timestamp: "current",
          },
          ...metrics.by_route.slice(0, 6).map((route) => ({
            id: route.route,
            primary: route.route,
            secondary: `${route.requests.toLocaleString("en")} requests`,
            status: route.error_count > 0 ? "errors" : "healthy",
            tone: route.error_count > 0 ? "warn" : "ok" as StatusTone,
            metric: duration(route.avg_ms),
            timestamp: `${route.error_count} errors`,
          })),
        ]
      : seededSnapshot.observability

  const adapterRowsLive: AdapterSlot[] = [
    ...(adapterRegistry?.slots.map((slot) => {
      const capCount = slot.active.capabilities ? Object.keys(slot.active.capabilities).length : 0
      const swap = slot.swap_env ? `swap: ${slot.swap_env}` : slot.swap_method
      const adapter =
        slot.slot === "llm-chat"
          ? slot.active.capabilities?.virtual_keys_active === true
            ? "LiteLLM (virtual-keys)"
            : "LiteLLM (stateless)"
          : slot.active.name
      return {
        slot: slot.slot,
        adapter,
        status: toneForAdapterStatus(slot.active.status),
        description: `tier ${slot.tier} · ${slot.active.kind} · ${swap}${capCount ? ` · ${capCount} caps` : ""}`,
      }
    }) ?? []),
    ...(toolAdapters?.adapters.map((adapter) => ({
      slot: adapter.label,
      adapter: adapter.id,
      status: (adapter.configured && adapter.enabled ? "ok" : adapter.configured ? "running" : "warn") as StatusTone,
      description: adapter.description,
    })) ?? []),
    ...(nativeTools?.tools.map((tool) => ({
      slot: `Native ${tool.tool}`,
      adapter: tool.adapter_id,
      status: (tool.configured && tool.enabled ? "ok" : tool.configured ? "running" : "warn") as StatusTone,
      description: tool.description,
    })) ?? []),
    ...(harnesses?.harnesses.map((harness) => ({
      slot: `Harness ${harness.provider}`,
      adapter: harness.binary_path ?? "missing",
      status: toneForStatus(harness.status),
      description: harness.version ?? harness.last_error ?? "CLI harness",
    })) ?? []),
  ]

  const deployRows: ConsoleRow[] = [
    ...seededSnapshot.deployTargets,
    ...(crons?.crons.slice(0, 4).map((cron) => ({
      id: `cron:${cron.id}`,
      primary: cron.name,
      secondary: `${cron.job_name} · ${cron.schedule}`,
      status: cron.is_active ? "active" : "disabled",
      tone: cron.is_active ? "running" : "neutral" as StatusTone,
      metric: shortTime(cron.next_run_at),
      timestamp: cron.last_run_at ? shortTime(cron.last_run_at) : "never run",
    })) ?? []),
    ...(plugins?.plugins.slice(0, 4).map((plugin) => ({
      id: `plugin:${plugin.id}`,
      primary: plugin.name,
      secondary: `${plugin.group} · ${plugin.route}`,
      status: "installed",
      tone: "ok" as StatusTone,
      metric: plugin.version,
      timestamp: "build",
    })) ?? []),
  ]

  const serviceRowsLive: ServiceVital[] =
    connectedServices?.services.map((service) => {
      const status = service.status === "healthy" || service.status === "degraded" || service.status === "offline" || service.status === "configured"
        ? service.status
        : toneForStatus(service.status) === "fail" ? "offline" : "degraded"
      return {
        name: service.name,
        version: service.version ?? service.kind,
        status,
        checked: shortTime(service.checked_at),
        adapter: service.purpose,
        href: service.admin_url ?? undefined,
      }
    }) ?? []

  const dbHealthRows: ConsoleRow[] = dbHealth
    ? [
        {
          id: "db-connections",
          primary: "Connections",
          secondary: `${dbHealth.connections.active} active · ${dbHealth.connections.idle} idle`,
          status: dbHealth.available ? "available" : "degraded",
          tone: dbHealth.available ? "ok" : "warn",
          metric: `${dbHealth.connections.max} max`,
          timestamp: shortTime(dbHealth.checked_at),
        },
        {
          id: "db-cache-hit",
          primary: "Cache hit ratio",
          secondary: dbHealth.reason ?? "pg_statio_user_tables",
          status: dbHealth.available ? "healthy" : "limited",
          tone: dbHealth.available ? "ok" : "warn",
          metric: `${Math.round(dbHealth.cache_hit_ratio * 100)}%`,
          timestamp: "current",
        },
        ...dbHealth.slow_queries.slice(0, 4).map((query, index) => ({
          id: `db-slow-${index}`,
          primary: query.query,
          secondary: `${query.calls.toLocaleString("en")} calls · ${Math.round(query.total_ms).toLocaleString("en")} ms total`,
          status: query.mean_ms > 1000 ? "slow" : "tracked",
          tone: query.mean_ms > 1000 ? "warn" : "running" as StatusTone,
          metric: duration(query.mean_ms),
          timestamp: "pg_stat",
        })),
        ...dbHealth.largest_tables.slice(0, 4).map((table) => ({
          id: `db-table-${table.schema}.${table.table}`,
          primary: `${table.schema}.${table.table}`,
          secondary: `${table.row_count.toLocaleString("en")} estimated rows`,
          status: "largest",
          tone: "neutral" as StatusTone,
          metric: bytes(table.size_bytes),
          timestamp: "storage",
        })),
        ...dbHealth.locks.slice(0, 4).map((lock) => ({
          id: `db-lock-${lock.pid}-${lock.mode}`,
          primary: lock.relation || `pid ${lock.pid}`,
          secondary: lock.mode,
          status: lock.granted ? "granted" : "waiting",
          tone: lock.granted ? "ok" : "warn" as StatusTone,
          metric: duration(lock.age_ms),
          timestamp: "lock",
        })),
      ]
    : seededSnapshot.dbHealth

  const providerHealthRows: ConsoleRow[] =
    providerHealth?.providers.map((provider) => ({
      id: `provider-health-${provider.provider}`,
      primary: provider.provider,
      secondary: `${provider.observations.toLocaleString("en")} observations · p95 ${duration(provider.p95_latency_ms)}`,
      status: provider.status,
      tone: toneForStatus(provider.status),
      metric: `${provider.availability_pct.toFixed(1)}%`,
      timestamp: provider.last_observed_at ? shortTime(provider.last_observed_at) : providerHealth.window,
    })) ?? seededSnapshot.providerHealth

  const notificationMuteRows: ConsoleRow[] =
    notificationMutes?.mutes.map((mute) => ({
      id: mute.id,
      primary: `${mute.pattern.kind}:${mute.pattern.template}`,
      secondary: `${mute.pattern.recipient} · category ${mute.pattern.category}`,
      status: mute.expires_at ? "expires" : "active",
      tone: mute.expires_at ? "warn" : "ok",
      metric: mute.reason ?? "mute",
      timestamp: mute.expires_at ? shortTime(mute.expires_at) : shortTime(mute.created_at),
    })) ?? seededSnapshot.notificationMutes

  const searchIndexRows: ConsoleRow[] =
    searchIndexStats?.indexes.map((index) => ({
      id: `${index.schema}.${index.index}`,
      primary: index.index,
      secondary: `${index.schema}.${index.table}`,
      status: index.index_scans > 0 ? "used" : "idle",
      tone: index.index_scans > 0 ? "ok" : "neutral",
      metric: `${bytes(index.size_bytes)} · ${index.index_scans.toLocaleString("en")} scans`,
      timestamp: index.last_vacuum ? `vacuum ${shortTime(index.last_vacuum)}` : "no vacuum",
      detail: index.definition,
    })) ?? seededSnapshot.searchIndexes

  const brandRows: ConsoleRow[] = brand
    ? [
        {
          id: "brand-source",
          primary: "Source",
          secondary: brand.source,
          status: brand.override ? "override" : "file",
          tone: brand.override ? "running" : "neutral",
          metric: brand.brand_yaml_path,
          timestamp: brand.updated_at ? shortTime(brand.updated_at) : "file",
        },
        ...Object.entries(brand.brand).map(([key, value]) => ({
          id: `brand-${key}`,
          primary: key,
          secondary: displayValue(value),
          status: brand.override && Object.prototype.hasOwnProperty.call(brand.override, key) ? "override" : "file",
          tone: brand.override && Object.prototype.hasOwnProperty.call(brand.override, key) ? "running" as StatusTone : "neutral" as StatusTone,
          metric: typeof value,
          timestamp: brand.updated_at ? shortTime(brand.updated_at) : "current",
        })),
        {
          id: "brand-apply",
          primary: "Apply behavior",
          secondary: brand.apply,
          status: "restart",
          tone: "warn",
          metric: "required",
          timestamp: "policy",
        },
      ]
    : seededSnapshot.brand

  const featureRows: ConsoleRow[] = features
    ? Object.entries(features.features).map(([name, feature]) => {
        const details = feature.details?.map((detail) => `${detail.key}=${displayValue(detail.value)} · ${detail.severity} · ${detail.message}`).join("; ")
        const metric =
          feature.adapter ??
          feature.backend ??
          (feature.enabled !== undefined ? (feature.enabled ? "enabled" : "disabled") : undefined) ??
          (feature.virtual_keys ? "virtual keys" : "local")
        return {
          id: `feature-${name}`,
          primary: name,
          secondary: details || "No capability caveats reported.",
          status: feature.capability_status,
          tone: toneForFeatureStatus(feature.capability_status),
          metric,
          timestamp: features.preset,
        }
      })
    : seededSnapshot.features

  const featureWarningRows: ConsoleRow[] =
    features?.validator_warnings.map((warning) => ({
      id: `feature-warning-${warning.feature}`,
      primary: warning.feature,
      secondary: warning.message,
      status: warning.level,
      tone: warning.level === "error" ? "fail" : "warn",
      metric: warning.remediation,
      timestamp: "validator",
    })) ?? []

  return {
    ...seededSnapshot,
    source: "live",
    generatedAt: nowIso(),
    runtimeStatus: health?.status ?? "reachable",
    endpointStatus: {
      logs: logs ? "ok" : "missing",
      traces: "degraded",
      adapters: adapterRegistry?.slots.length ? "ok" : adapterRowsLive.length ? "degraded" : "missing",
      features: features ? "ok" : "missing",
      auth: "degraded",
      llm: models ? "degraded" : "missing",
      deployTargets: "missing",
      brand: brand ? "ok" : "missing",
    },
    kpis: [
      {
        ...seededSnapshot.kpis[0],
        value: home ? home.requests_per_minute.toLocaleString("en") : seededSnapshot.kpis[0].value,
        sparkline: home?.request_sparkline ?? seededSnapshot.kpis[0].sparkline,
      },
      {
        ...seededSnapshot.kpis[1],
        value: home ? `${home.error_rate.toFixed(1)}%` : seededSnapshot.kpis[1].value,
        sparkline: home?.error_sparkline ?? seededSnapshot.kpis[1].sparkline,
      },
      {
        ...seededSnapshot.kpis[2],
        value: money(home?.cost_today_usd ?? cost?.period_total_usd),
        sparkline: costSparkline?.length ? costSparkline : home?.cost_sparkline ?? seededSnapshot.kpis[2].sparkline,
      },
      {
        ...seededSnapshot.kpis[3],
        value: money(cost?.period_total_usd ?? 0),
        detail: cost?.budget_usd ? `${money(cost.budget_usd)} budget` : "no budget set",
      },
      {
        ...seededSnapshot.kpis[4],
        value: String(home?.queue_depth ?? queue?.pending ?? 0),
        sparkline: home?.queue_sparkline ?? seededSnapshot.kpis[4].sparkline,
      },
      {
        ...seededSnapshot.kpis[5],
        value: String(runRows.filter((run) => run.status === "running").length),
      },
      {
        ...seededSnapshot.kpis[6],
        value: String(runRows.filter((run) => run.status === "failed").length),
      },
      {
        ...seededSnapshot.kpis[7],
        value: cost?.budget_usd ? `${Math.round((cost.period_total_usd / cost.budget_usd) * 100)}%` : "n/a",
        detail: cost?.budget_usd ? "selected scope" : "set budgets to track",
      },
    ],
    activity: [
      ...runRows.slice(0, 8),
      ...(home?.alerts.map((alert) => ({
        id: alert.id,
        primary: alert.title,
        secondary: alert.description ?? "platform alert",
        status: alert.severity,
        tone: toneForStatus(alert.severity),
        metric: "alert",
        timestamp: "now",
      })) ?? []),
    ],
    runs: runRows,
    costRows: costEventRows,
    tenants: tenantRows,
    apiKeys:
      keys?.keys.map((key) => {
        const mirror = key.litellm_key_alias ? "mirrored" : "local only"
        return {
          id: key.id,
          primary: key.name ?? key.prefix,
          secondary: `${key.tenant_id ?? "platform"} · ${key.prefix} · ${mirror}`,
          status: key.revoked_at ? "revoked" : "active",
          tone: key.revoked_at ? "fail" : "ok",
          metric: key.live_spend_usd == null ? mirror : `${money(key.live_spend_usd)} · ${mirror}`,
          timestamp: shortTime(key.created_at),
        }
      }) ?? seededSnapshot.apiKeys,
    members:
      users?.users.map((user) => ({
        id: user.id,
        primary: user.email,
        secondary: user.name ?? "member",
        status: user.deleted_at ? "deleted" : "active",
        tone: user.deleted_at ? "fail" : "ok",
        metric: user.created_at ? shortTime(user.created_at) : "created",
        timestamp: user.deleted_at ? shortTime(user.deleted_at) : "current",
      })) ?? seededSnapshot.members,
    agents:
      agents?.agents.map((agent) => ({
        id: agent.node_id,
        primary: agent.node_id,
        secondary: `reasoners: ${agent.reasoners?.join(", ") || "default"} · tags: ${agent.tags?.join(", ") || "none"}`,
        status: "healthy",
        tone: "ok",
        metric: agent.version ?? "version n/a",
        timestamp: "runtime",
      })) ?? seededSnapshot.agents,
    tables:
      tables?.tables.map((table) => ({
        id: `${table.schema}.${table.name}`,
        primary: `${table.schema}.${table.name}`,
        secondary: `${table.kind} · ${Math.round(table.size_bytes / 1024).toLocaleString("en")} KiB`,
        status: table.has_rls ? "rls on" : "rls off",
        tone: table.has_rls ? "ok" : "warn",
        metric: `${table.estimated_rows.toLocaleString("en")} rows`,
        timestamp: "schema",
      })) ?? seededSnapshot.tables,
    dbHealth: dbHealthRows,
    queue: jobRows.length ? jobRows : (
      queue?.recent.map((job) => ({
          id: job.id,
          primary: job.name,
          secondary: job.last_error ?? "no error",
          status: job.status,
          tone: toneForStatus(job.status),
          metric: `${job.attempts} attempts`,
          timestamp: shortTime(job.enqueued_at),
        })) ?? seededSnapshot.queue
    ),
    errors: logRows
      .filter((line) => line.status === "error")
      .slice(0, 20)
      .map((line) => ({
        ...line,
        primary: line.primary || "Runtime error",
        secondary: `${line.secondary} · grouped client-side from logs`,
        href: `/operate/errors?log=${encodeURIComponent(line.id)}`,
      })),
    traces: traceRows,
    logs: logRows,
    cache: cacheRows,
    webhooks:
      webhooks?.deliveries.map((delivery) => ({
        id: delivery.id,
        primary: delivery.event_type,
        secondary: `${delivery.direction} · ${delivery.destination}`,
        status: delivery.status,
        tone: toneForStatus(delivery.status),
        metric: delivery.response_status ? String(delivery.response_status) : "pending",
        timestamp: shortTime(delivery.created_at),
      })) ?? seededSnapshot.webhooks,
    webhookEndpoints: webhookEndpointRows,
    approvals: approvalRows,
    audit:
      audit?.entries.map((entry) => ({
        id: entry.id,
        primary: entry.action,
        secondary: `${entry.user_id ?? entry.api_key_id ?? "system"} · ${entry.resource_type ?? "platform"}`,
        status: "recorded",
        tone: "ok",
        metric: entry.tenant_id ?? "platform",
        timestamp: shortTime(entry.occurred_at),
        href: `/customers/audit?entry=${encodeURIComponent(entry.id)}`,
        detail: JSON.stringify(entry.metadata, null, 2),
      })) ?? seededSnapshot.audit,
    oauth: oauthRows,
    sessions: sessionRows.length ? sessionRows : seededSnapshot.sessions,
    budgets:
      budgets?.budgets.map((budget) => ({
        tenant: budget.tenant_id,
        cap: money(budget.monthly_usd),
        used: budget.monthly_usd ? Math.min(100, Math.round((budget.spent_this_period_usd / budget.monthly_usd) * 100)) : 0,
        status:
          budget.spent_this_period_usd > budget.monthly_usd
            ? "fail"
            : budget.monthly_usd > 0 && budget.spent_this_period_usd / budget.monthly_usd >= budget.alert_threshold_pct / 100
          ? "warn"
              : "ok",
      })) ?? seededSnapshot.budgets,
    jobs: jobRows,
    jobDefinitions: jobDefinitionRows,
    storage: storageRows,
    memory: memoryRows,
    searchIndexes: searchIndexRows,
    modules: moduleRows,
    skills: skillRows.length ? skillRows : seededSnapshot.skills,
    tools: toolRows.length ? toolRows : seededSnapshot.tools,
    reasoners: reasonerRows,
    harnesses: harnessRows,
    crons: cronRows,
    featureFlags: flagRows,
    billing: billingRows,
    secrets: secretRows,
    notifications: notificationRows,
    notificationMutes: notificationMuteRows,
    llmProviders: providerHealthRows.length ? [...providerHealthRows, ...llmRows].slice(0, 20) : llmRows,
    providerHealth: providerHealthRows,
    sandbox: sandboxRows,
    observability: observabilityRows,
    deployTargets: deployRows,
    adapters: adapterRowsLive.length
      ? adapterRowsLive
      : models || cacheStats
        ? seededAdapters.map((slot) => (
            slot.slot === "LLM gateway" ? { ...slot, status: "ok" } : slot
          ))
        : seededAdapters,
    logCapabilities: logCapabilities ?? seededSnapshot.logCapabilities,
    traceCapabilities: traceCapabilities ?? seededSnapshot.traceCapabilities,
    metricsCapabilities: metricsCapabilities ?? seededSnapshot.metricsCapabilities,
    costSeries: costSeriesRows.length ? costSeriesRows : seededSnapshot.costSeries,
    containerMetrics: containerMetricRows.length ? containerMetricRows : seededSnapshot.containerMetrics,
    services: serviceRowsLive.length ? serviceRowsLive : seededSnapshot.services,
    brand: brandRows,
    features: featureRows,
    featureWarnings: featureWarningRows,
  }
}
