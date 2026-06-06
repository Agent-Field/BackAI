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
    // On the server, prefer internal Docker DNS / loopback.
    return process.env.RUNTIME_URL ?? "http://localhost:8080"
  }
  // In the browser, go through the dashboard's same-origin proxy
  // (see next.config.ts rewrites). This avoids CORS and keeps the
  // runtime URL invisible to the client.
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

// ─── Jobs (Phase 5) ───────────────────────────────────────────────────────

export const JobSchema = z.object({
  id: z.string(),
  name: z.string(),
  args: z.unknown(),
  state: z.enum([
    "available",
    "running",
    "completed",
    "discarded",
    "cancelled",
    "retryable",
    "scheduled",
    "pending",
  ]),
  tenant_id: z.string().nullable(),
  attempt: z.number(),
  max_attempts: z.number(),
  scheduled_at: z.string(),
  enqueued_at: z.string(),
  attempted_at: z.string().nullable(),
  finalized_at: z.string().nullable(),
  errors: z
    .array(
      z.object({
        at: z.string(),
        attempt: z.number(),
        error: z.string(),
      }),
    )
    .nullable(),
})
export type Job = z.infer<typeof JobSchema>

export const JobListSchema = z.object({
  jobs: z.array(JobSchema),
  total: z.number(),
  has_more: z.boolean(),
})
export type JobList = z.infer<typeof JobListSchema>

export const JobDefinitionSchema = z.object({
  name: z.string(),
  language: z.enum(["python", "typescript", "go"]),
  source_path: z.string().nullable(),
  description: z.string().nullable(),
  cron: z.string().nullable(),
  recent: z.object({
    succeeded: z.number(),
    failed: z.number(),
    running: z.number(),
  }),
})
export type JobDefinition = z.infer<typeof JobDefinitionSchema>

export const JobDefinitionListSchema = z.object({
  definitions: z.array(JobDefinitionSchema),
})
export type JobDefinitionList = z.infer<typeof JobDefinitionListSchema>

export const EnqueueJobInputSchema = z.object({
  name: z.string(),
  args: z.unknown(),
  scheduled_at: z.string().optional(),
  max_attempts: z.number().int().min(1).max(50).optional(),
})
export type EnqueueJobInput = z.infer<typeof EnqueueJobInputSchema>

// ─── Secrets (Phase 5) ────────────────────────────────────────────────────

export const SecretMetadataSchema = z.object({
  key: z.string(),
  tenant_id: z.string().nullable(),
  description: z.string().nullable(),
  rotate_after: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
  // value is NEVER returned in list/show responses; only via /secrets/{key}/reveal
})
export type SecretMetadata = z.infer<typeof SecretMetadataSchema>

export const SecretListSchema = z.object({
  secrets: z.array(SecretMetadataSchema),
})
export type SecretList = z.infer<typeof SecretListSchema>

export const SecretValueSchema = z.object({
  key: z.string(),
  value: z.string(),
})
export type SecretValue = z.infer<typeof SecretValueSchema>

export const PutSecretInputSchema = z.object({
  value: z.string(),
  description: z.string().optional(),
  rotate_after: z.string().optional(),
})
export type PutSecretInput = z.infer<typeof PutSecretInputSchema>

// ─── Storage (Phase 5) ────────────────────────────────────────────────────

export const StorageObjectSchema = z.object({
  key: z.string(),
  size: z.number(),
  content_type: z.string().nullable(),
  last_modified: z.string(),
  etag: z.string().nullable(),
})
export type StorageObject = z.infer<typeof StorageObjectSchema>

export const StorageListSchema = z.object({
  objects: z.array(StorageObjectSchema),
  prefix: z.string(),
  next_token: z.string().nullable(),
})
export type StorageList = z.infer<typeof StorageListSchema>

export const SignedURLSchema = z.object({
  key: z.string(),
  url: z.string(),
  expires_at: z.string(),
})
export type SignedURL = z.infer<typeof SignedURLSchema>

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

  // ─── Jobs ───
  jobs: {
    list: (params?: {
      name?: string
      state?: string
      tenant?: string
      limit?: number
      offset?: number
    }) => {
      const qs = new URLSearchParams()
      if (params?.name) qs.set("name", params.name)
      if (params?.state) qs.set("state", params.state)
      if (params?.tenant) qs.set("tenant", params.tenant)
      if (params?.limit !== undefined) qs.set("limit", String(params.limit))
      if (params?.offset !== undefined) qs.set("offset", String(params.offset))
      const q = qs.toString()
      return request(`/api/v1/jobs${q ? "?" + q : ""}`, undefined, JobListSchema)
    },
    get: (id: string) => request(`/api/v1/jobs/${id}`, undefined, JobSchema),
    enqueue: (input: EnqueueJobInput) =>
      request("/api/v1/jobs", { method: "POST", json: input }, JobSchema),
    retry: (id: string) =>
      request(`/api/v1/jobs/${id}/retry`, { method: "POST" }, JobSchema),
    definitions: () =>
      request("/api/v1/jobs/definitions", undefined, JobDefinitionListSchema),
  },

  // ─── Secrets ───
  secrets: {
    list: () => request("/api/v1/secrets", undefined, SecretListSchema),
    get: (key: string) =>
      request(
        `/api/v1/secrets/${encodeURIComponent(key)}`,
        undefined,
        SecretMetadataSchema,
      ),
    reveal: (key: string) =>
      request(
        `/api/v1/secrets/${encodeURIComponent(key)}/reveal`,
        { method: "POST" },
        SecretValueSchema,
      ),
    put: (key: string, input: PutSecretInput) =>
      request(
        `/api/v1/secrets/${encodeURIComponent(key)}`,
        { method: "PUT", json: input },
        SecretMetadataSchema,
      ),
    delete: (key: string) =>
      request(
        `/api/v1/secrets/${encodeURIComponent(key)}`,
        { method: "DELETE" },
        z.object({ deleted: z.boolean() }),
      ),
    rotate: (key: string, input: { value: string }) =>
      request(
        `/api/v1/secrets/${encodeURIComponent(key)}/rotate`,
        { method: "POST", json: input },
        SecretMetadataSchema,
      ),
  },

  // ─── Storage ───
  storage: {
    list: (params?: { prefix?: string; next_token?: string; limit?: number }) => {
      const qs = new URLSearchParams()
      if (params?.prefix) qs.set("prefix", params.prefix)
      if (params?.next_token) qs.set("next_token", params.next_token)
      if (params?.limit !== undefined) qs.set("limit", String(params.limit))
      const q = qs.toString()
      return request(
        `/api/v1/storage${q ? "?" + q : ""}`,
        undefined,
        StorageListSchema,
      )
    },
    signedURL: (key: string, ttlSeconds?: number) => {
      const qs = new URLSearchParams({ key })
      if (ttlSeconds !== undefined) qs.set("ttl", String(ttlSeconds))
      return request(
        `/api/v1/storage/signed-url?${qs.toString()}`,
        undefined,
        SignedURLSchema,
      )
    },
    delete: (key: string) =>
      request(
        `/api/v1/storage/${encodeURIComponent(key)}`,
        { method: "DELETE" },
        z.object({ deleted: z.boolean() }),
      ),
    // Upload uses a raw fetch (multipart body); not run through `request()`
    // because the schema layer doesn't help with FormData.
    upload: async (key: string, file: File | Blob): Promise<StorageObject> => {
      const form = new FormData()
      form.set("key", key)
      form.set("file", file)
      const res = await fetch("/api/v1/storage/upload", {
        method: "POST",
        body: form,
        credentials: "include",
      })
      if (!res.ok) {
        const text = await res.text()
        throw new ApiError(`upload failed: ${text}`, res.status, "UPLOAD_FAILED")
      }
      return StorageObjectSchema.parse(await res.json())
    },
  },
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
