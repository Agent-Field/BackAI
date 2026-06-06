// Single typed client for the suite runtime REST API.
//
// Every fetch from the dashboard goes through `api`. Adding a new endpoint
// means editing exactly one file. zod schemas validate the responses at the
// network boundary so the rest of the app works with safe types.
//
// On the server, calls go directly to the runtime (env: RUNTIME_URL).
// On the client, calls go through Next.js relative paths so they pick up
// the dashboard's auth cookie.

import { z } from "zod"

const isServer = typeof window === "undefined"

function baseUrl(): string {
  if (isServer) {
    return process.env.RUNTIME_URL ?? "http://localhost:8080"
  }
  return ""
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details?: unknown
  constructor(message: string, status: number, code: string, details?: unknown) {
    super(message)
    this.status = status
    this.code = code
    this.details = details
  }
}

type RequestInitWithJson = Omit<RequestInit, "body"> & { json?: unknown }

async function request<T>(
  path: string,
  init: RequestInitWithJson | undefined,
  schema: z.ZodType<T>,
): Promise<T> {
  const headers = new Headers(init?.headers)
  let body: BodyInit | undefined
  if (init?.json !== undefined) {
    headers.set("Content-Type", "application/json")
    body = JSON.stringify(init.json)
  }
  const res = await fetch(`${baseUrl()}${path}`, {
    ...init,
    headers,
    body,
    credentials: "include",
    cache: "no-store",
  })
  const text = await res.text()
  let parsed: unknown
  if (text.length > 0) {
    try {
      parsed = JSON.parse(text)
    } catch {
      throw new ApiError(`Non-JSON response from ${path}`, res.status, "BAD_RESPONSE")
    }
  }
  if (!res.ok) {
    const envelope = parsed as { error?: { code?: string; message?: string; details?: unknown } }
    throw new ApiError(
      envelope?.error?.message ?? `HTTP ${res.status}`,
      res.status,
      envelope?.error?.code ?? "HTTP_ERROR",
      envelope?.error?.details,
    )
  }
  const validated = schema.safeParse(parsed)
  if (!validated.success) {
    throw new ApiError(
      `Response from ${path} did not match schema: ${validated.error.message}`,
      res.status,
      "SCHEMA_MISMATCH",
      validated.error.issues,
    )
  }
  return validated.data
}

// ─── Schemas ──────────────────────────────────────────────────────────────

export const HealthSchema = z.object({
  status: z.string(),
  started_at: z.string().optional(),
  uptime_s: z.number().optional(),
  checks: z.record(z.string(), z.unknown()).optional(),
})
export type Health = z.infer<typeof HealthSchema>

export const AgentInfoSchema = z.object({
  node_id: z.string(),
  version: z.string().optional(),
  tags: z.array(z.string()).optional(),
  reasoners: z.array(z.string()).optional(),
})
export type AgentInfo = z.infer<typeof AgentInfoSchema>

export const AgentListSchema = z.object({
  agents: z.array(AgentInfoSchema),
})
export type AgentList = z.infer<typeof AgentListSchema>

export const RunSchema = z.object({
  id: z.string(),
  agent: z.string(),
  status: z.enum(["queued", "running", "succeeded", "failed", "cancelled"]),
  tenant_id: z.string().optional(),
  started_at: z.string(),
  duration_ms: z.number().optional(),
  cost_usd: z.number().optional(),
  input: z.unknown().optional(),
  output: z.unknown().optional(),
  error: z.string().optional(),
})
export type Run = z.infer<typeof RunSchema>

export const RunListSchema = z.object({
  runs: z.array(RunSchema),
  total: z.number(),
  has_more: z.boolean(),
})
export type RunList = z.infer<typeof RunListSchema>

export const CostPointSchema = z.object({
  date: z.string(),
  cost_usd: z.number(),
})
export type CostPoint = z.infer<typeof CostPointSchema>

export const CostSummarySchema = z.object({
  period_total_usd: z.number(),
  previous_total_usd: z.number(),
  budget_usd: z.number().nullable(),
  forecast_usd: z.number(),
  by_day: z.array(CostPointSchema),
  by_model: z.array(
    z.object({
      model: z.string(),
      cost_usd: z.number(),
    }),
  ),
  by_agent: z.array(
    z.object({
      agent: z.string(),
      cost_usd: z.number(),
    }),
  ),
  by_tenant: z.array(
    z.object({
      tenant_id: z.string(),
      tenant_name: z.string().nullable(),
      cost_usd: z.number(),
    }),
  ),
})
export type CostSummary = z.infer<typeof CostSummarySchema>

export const HomeOverviewSchema = z.object({
  requests_per_minute: z.number(),
  error_rate: z.number(),
  cost_today_usd: z.number(),
  queue_depth: z.number(),
  request_sparkline: z.array(z.number()),
  error_sparkline: z.array(z.number()),
  cost_sparkline: z.array(z.number()),
  queue_sparkline: z.array(z.number()),
  recent_runs: z.array(RunSchema),
  recent_webhook_deliveries: z.array(
    z.object({
      id: z.string(),
      url: z.string(),
      direction: z.enum(["in", "out"]),
      status: z.enum(["delivered", "failed", "pending"]),
      occurred_at: z.string(),
    }),
  ),
  alerts: z.array(
    z.object({
      id: z.string(),
      severity: z.enum(["info", "warning", "critical"]),
      title: z.string(),
      description: z.string().optional(),
    }),
  ),
})
export type HomeOverview = z.infer<typeof HomeOverviewSchema>

export const LogLineSchema = z.object({
  ts: z.string(),
  level: z.enum(["debug", "info", "warn", "error"]),
  service: z.string(),
  msg: z.string(),
  request_id: z.string().optional(),
  tenant_id: z.string().optional(),
  agent: z.string().optional(),
})
export type LogLine = z.infer<typeof LogLineSchema>

export const QueueSummarySchema = z.object({
  pending: z.number(),
  running: z.number(),
  failed: z.number(),
  succeeded_today: z.number(),
  recent: z.array(
    z.object({
      id: z.string(),
      name: z.string(),
      status: z.enum(["pending", "running", "succeeded", "failed", "cancelled"]),
      enqueued_at: z.string(),
      attempts: z.number(),
      last_error: z.string().nullable(),
    }),
  ),
})
export type QueueSummary = z.infer<typeof QueueSummarySchema>

export const ModulesStateSchema = z.object({
  modules: z.array(
    z.object({
      id: z.string(),
      name: z.string(),
      enabled: z.boolean(),
      adapter: z.string().optional(),
      version: z.string().optional(),
    }),
  ),
  workload_modules: z.array(z.string()),
  multi_tenancy_enabled: z.boolean(),
})
export type ModulesState = z.infer<typeof ModulesStateSchema>

// ─── Client ───────────────────────────────────────────────────────────────

export const api = {
  health: () => request("/health", undefined, HealthSchema),
  agents: () => request("/api/v1/agents", undefined, AgentListSchema),
  runs: (params?: {
    agent?: string
    tenant?: string
    status?: string
    limit?: number
    offset?: number
  }) => {
    const qs = new URLSearchParams()
    if (params?.agent) qs.set("agent", params.agent)
    if (params?.tenant) qs.set("tenant", params.tenant)
    if (params?.status) qs.set("status", params.status)
    if (params?.limit !== undefined) qs.set("limit", String(params.limit))
    if (params?.offset !== undefined) qs.set("offset", String(params.offset))
    const q = qs.toString()
    return request(`/api/v1/runs${q ? "?" + q : ""}`, undefined, RunListSchema)
  },
  cost: (params?: { from?: string; to?: string }) => {
    const qs = new URLSearchParams()
    if (params?.from) qs.set("from", params.from)
    if (params?.to) qs.set("to", params.to)
    const q = qs.toString()
    return request(`/api/v1/cost${q ? "?" + q : ""}`, undefined, CostSummarySchema)
  },
  home: () => request("/api/v1/home/overview", undefined, HomeOverviewSchema),
  modulesState: () => request("/api/v1/modules", undefined, ModulesStateSchema),
  queue: () => request("/api/v1/queues/summary", undefined, QueueSummarySchema),
  // Build URL pattern for "View full trace" link-out (to runtime UI).
  traceUrl(executionId: string): string {
    const runtimeUI = process.env.NEXT_PUBLIC_RUNTIME_UI_URL ?? "http://localhost:8081"
    return `${runtimeUI}/?execution=${encodeURIComponent(executionId)}`
  },
}

// ─── Multi-tenancy helpers ────────────────────────────────────────────────

/**
 * Whether the multi-tenancy module is enabled. Read via `api.modulesState`.
 * Used to decide whether `Customers/*` tabs render real content or the
 * "Enable multi-tenancy" empty state.
 */
export async function isMultiTenancyEnabled(): Promise<boolean> {
  try {
    const state = await api.modulesState()
    return state.multi_tenancy_enabled
  } catch {
    return false
  }
}
