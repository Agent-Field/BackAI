// SPDX-License-Identifier: Apache-2.0

// suite.jobs.* — background job operations.
//
// Endpoint paths and JSON shapes are the canonical contract from
// `apps/dashboard/src/lib/api.ts` (the Phase 5 Jobs section). Schemas
// below mirror the zod schemas there; if the dashboard contract moves,
// move these together.
//
// Public API stays camelCase; we translate snake_case <-> camelCase at the
// wire boundary using helpers in `_http.ts`.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

// ---------- shared schemas (mirror lib/api.ts) ----------

export const JobStateSchema = z.enum([
  "available",
  "running",
  "completed",
  "discarded",
  "cancelled",
  "retryable",
  "scheduled",
  "pending",
])
export type JobState = z.infer<typeof JobStateSchema>

const JobErrorSchema = z.object({
  at: z.string(),
  attempt: z.number(),
  error: z.string(),
})
export type JobError = z.infer<typeof JobErrorSchema>

export const JobSchema = z.object({
  id: z.string(),
  name: z.string(),
  args: z.unknown(),
  state: JobStateSchema,
  tenantId: z.string().nullable(),
  attempt: z.number(),
  maxAttempts: z.number(),
  scheduledAt: z.string(),
  enqueuedAt: z.string(),
  attemptedAt: z.string().nullable(),
  finalizedAt: z.string().nullable(),
  errors: z.array(JobErrorSchema).nullable(),
})
export type Job = z.infer<typeof JobSchema>

export const JobListSchema = z.object({
  jobs: z.array(JobSchema),
  total: z.number(),
  hasMore: z.boolean(),
})
export type JobList = z.infer<typeof JobListSchema>

// ---------- public API ----------

export interface EnqueueJobOptions extends HttpOptions {
  /** ISO 8601 / Date — defer execution until this moment. */
  scheduledAt?: Date | string
  /** Override the worker's default attempt cap (1–50). */
  maxAttempts?: number
}

export interface ListJobsOptions extends HttpOptions {
  name?: string
  state?: JobState | string
  tenant?: string
  limit?: number
  offset?: number
}

/** Enqueue a background job. Returns the persisted row. */
export async function enqueue(
  name: string,
  args: unknown,
  opts: EnqueueJobOptions = {},
): Promise<Job> {
  if (typeof name !== "string" || name.length === 0) {
    throw new Error("job name must be a non-empty string")
  }
  const { scheduledAt, maxAttempts, ...http } = opts
  if (maxAttempts !== undefined && (maxAttempts < 1 || maxAttempts > 50)) {
    throw new Error("maxAttempts must be between 1 and 50")
  }
  const body: Record<string, unknown> = {
    name,
    args: args ?? {},
  }
  if (scheduledAt !== undefined) {
    body.scheduled_at = serializeDate(scheduledAt)
  }
  if (maxAttempts !== undefined) {
    body.max_attempts = maxAttempts
  }
  const raw = await request<unknown>("POST", "/jobs", body, http)
  return JobSchema.parse(camelizeJob(raw))
}

/** Fetch a job by id. */
export async function get(id: string, opts: HttpOptions = {}): Promise<Job> {
  if (typeof id !== "string" || id.length === 0) {
    throw new Error("job id must be a non-empty string")
  }
  const raw = await request<unknown>(
    "GET",
    `/jobs/${encodeURIComponent(id)}`,
    null,
    opts,
  )
  return JobSchema.parse(camelizeJob(raw))
}

/** Force-retry a job (resets state to `available`). */
export async function retry(id: string, opts: HttpOptions = {}): Promise<Job> {
  if (typeof id !== "string" || id.length === 0) {
    throw new Error("job id must be a non-empty string")
  }
  const raw = await request<unknown>(
    "POST",
    `/jobs/${encodeURIComponent(id)}/retry`,
    null,
    opts,
  )
  return JobSchema.parse(camelizeJob(raw))
}

/** List jobs with optional filters. */
export async function list(opts: ListJobsOptions = {}): Promise<JobList> {
  const { name, state, tenant, limit = 50, offset = 0, ...http } = opts
  const qs = new URLSearchParams()
  if (name !== undefined) qs.set("name", name)
  if (state !== undefined) qs.set("state", state)
  if (tenant !== undefined) qs.set("tenant", tenant)
  qs.set("limit", String(limit))
  qs.set("offset", String(offset))
  const raw = await request<unknown>("GET", `/jobs?${qs.toString()}`, null, http)
  return JobListSchema.parse(camelizeJobList(raw))
}

// ---------- helpers ----------

function serializeDate(value: Date | string): string {
  if (value instanceof Date) return value.toISOString()
  return value
}

/**
 * Translate the snake_case wire envelope into the camelCase shape declared
 * by `JobSchema`, *without* touching `args` (which is agent-defined and
 * forwarded verbatim). Mirrors the OPAQUE_FIELDS pattern in agents.ts.
 */
function camelizeJob(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(src)) {
    const ck = camelKey(k)
    if (ck === "args") {
      out.args = v
    } else {
      out[ck] = v
    }
  }
  return out
}

function camelizeJobList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const jobs = Array.isArray(src.jobs) ? src.jobs.map(camelizeJob) : src.jobs
  return {
    jobs,
    total: src.total,
    hasMore: src.has_more ?? src.hasMore,
  }
}

function camelKey(key: string): string {
  return key.replace(/_([a-z0-9])/g, (_, c: string) => c.toUpperCase())
}

/** Namespace object — the shape `suite.jobs` is built from. */
export const jobs = {
  enqueue,
  get,
  retry,
  list,
} as const