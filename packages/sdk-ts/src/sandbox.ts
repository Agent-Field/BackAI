// SPDX-License-Identifier: Apache-2.0

// suite.sandbox.* — code execution sandbox operations.
//
// Endpoint paths and JSON shapes are the canonical contract from
// `apps/dashboard/src/lib/api.ts` (the Phase 9 Sandbox section). Schemas
// below mirror the zod schemas there; if the dashboard contract moves,
// move these together.
//
// Public API stays camelCase; the wire stays snake_case. We translate at
// the boundary with helpers in this file — `files`, `env`, and
// `allowEgress` are forwarded verbatim because they are opaque
// caller-defined payloads.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

// ---------- shared schemas (mirror lib/api.ts, camelCase) ----------

export const SandboxStatusSchema = z.enum([
  "queued",
  "running",
  "done",
  "failed",
  "timeout",
  "killed",
])
export type SandboxStatus = z.infer<typeof SandboxStatusSchema>

export const SandboxNetworkSchema = z.enum(["open", "restricted", "isolated"])
export type SandboxNetwork = z.infer<typeof SandboxNetworkSchema>

export const SandboxCapabilitiesSchema = z.object({
  maxTimeoutS: z.number(),
  supportsGpu: z.boolean(),
  supportsNetwork: z.boolean(),
  supportsMounts: z.boolean(),
  coldStartMs: z.number(),
  adapter: z.string(),
})
export type SandboxCapabilities = z.infer<typeof SandboxCapabilitiesSchema>

export const SandboxRunSchema = z.object({
  id: z.string(),
  tenantId: z.string().nullable(),
  workspaceId: z.string().nullable(),
  adapter: z.string(),
  image: z.string(),
  command: z.array(z.string()),
  status: SandboxStatusSchema,
  exitCode: z.number().nullable(),
  durationS: z.number().nullable(),
  cpuSeconds: z.number().nullable(),
  memoryPeakMb: z.number().nullable(),
  networkBytesIn: z.number().nullable(),
  networkBytesOut: z.number().nullable(),
  costUsd: z.number().nullable(),
  stdoutUrl: z.string().nullable(),
  stderrUrl: z.string().nullable(),
  artifactsUrl: z.string().nullable(),
  startedAt: z.string().nullable(),
  endedAt: z.string().nullable(),
  createdAt: z.string(),
})
export type SandboxRun = z.infer<typeof SandboxRunSchema>

export const SandboxRunListSchema = z.object({
  runs: z.array(SandboxRunSchema),
  total: z.number(),
  hasMore: z.boolean(),
})
export type SandboxRunList = z.infer<typeof SandboxRunListSchema>

export const SandboxPoolStatsSchema = z.object({
  adapter: z.string(),
  warm: z.number(),
  active: z.number(),
  queued: z.number(),
  totalRunsToday: z.number(),
  cpuSecondsToday: z.number(),
  costUsdToday: z.number(),
  capabilities: SandboxCapabilitiesSchema,
})
export type SandboxPoolStats = z.infer<typeof SandboxPoolStatsSchema>

// ---------- public API ----------

const ALLOWED_NETWORKS: readonly SandboxNetwork[] = [
  "open",
  "restricted",
  "isolated",
]

export interface RunSandboxOptions extends HttpOptions {
  /** Files materialised into the container's working directory (path → text). */
  files?: Record<string, string>
  /** Environment variables injected into the container. */
  env?: Record<string, string>
  /** Wall-clock timeout in seconds (1–86400, default 300). */
  timeoutS?: number
  /** vCPU count (1–32, default 2). */
  cpu?: number
  /** Memory in GB (1–64, default 4). */
  memoryGb?: number
  /** Egress policy — defaults to `restricted`. */
  network?: SandboxNetwork | string
  /** Explicit allow-list of egress hostnames (honoured when network != isolated). */
  allowEgress?: string[]
  /** Workspace id for runs scoped to a workspace. */
  workspaceId?: string
}

export interface ListSandboxRunsOptions extends HttpOptions {
  tenant?: string
  status?: SandboxStatus | string
  limit?: number
  offset?: number
}

/** Submit a sandbox execution. Returns the persisted run row. */
export async function run(
  image: string,
  command: string[],
  opts: RunSandboxOptions = {},
): Promise<SandboxRun> {
  if (typeof image !== "string" || image.length === 0) {
    throw new Error("sandbox image must be a non-empty string")
  }
  if (!Array.isArray(command) || command.length === 0) {
    throw new Error("sandbox command must be a non-empty array of strings")
  }
  for (const arg of command) {
    if (typeof arg !== "string") {
      throw new Error("sandbox command entries must all be strings")
    }
  }

  const {
    files,
    env,
    timeoutS = 300,
    cpu = 2,
    memoryGb = 4,
    network = "restricted",
    allowEgress,
    workspaceId,
    ...http
  } = opts

  if (timeoutS < 1 || timeoutS > 86400) {
    throw new Error("timeoutS must be between 1 and 86400")
  }
  if (cpu < 1 || cpu > 32) {
    throw new Error("cpu must be between 1 and 32")
  }
  if (memoryGb < 1 || memoryGb > 64) {
    throw new Error("memoryGb must be between 1 and 64")
  }
  if (!(ALLOWED_NETWORKS as readonly string[]).includes(network)) {
    throw new Error(
      `network must be one of: ${ALLOWED_NETWORKS.join(", ")}; got ${JSON.stringify(network)}`,
    )
  }

  const body: Record<string, unknown> = {
    image,
    command: [...command],
    timeout_s: timeoutS,
    cpu,
    memory_gb: memoryGb,
    network,
  }
  if (files !== undefined) body.files = { ...files }
  if (env !== undefined) body.env = { ...env }
  if (allowEgress !== undefined) body.allow_egress = [...allowEgress]
  if (workspaceId !== undefined) body.workspace_id = workspaceId

  const raw = await request<unknown>("POST", "/sandbox/run", body, http)
  return SandboxRunSchema.parse(camelizeRun(raw))
}

/** List sandbox runs with optional filters. */
export async function list(
  opts: ListSandboxRunsOptions = {},
): Promise<SandboxRunList> {
  const { tenant, status, limit = 50, offset = 0, ...http } = opts
  if (limit <= 0) throw new Error("limit must be a positive integer")
  if (offset < 0) throw new Error("offset must be non-negative")
  const qs = new URLSearchParams()
  if (tenant !== undefined) qs.set("tenant", tenant)
  if (status !== undefined) qs.set("status", status)
  qs.set("limit", String(limit))
  qs.set("offset", String(offset))
  const raw = await request<unknown>(
    "GET",
    `/sandbox/runs?${qs.toString()}`,
    null,
    http,
  )
  return SandboxRunListSchema.parse(camelizeRunList(raw))
}

/** Fetch a sandbox run by id. */
export async function get(
  id: string,
  opts: HttpOptions = {},
): Promise<SandboxRun> {
  if (typeof id !== "string" || id.length === 0) {
    throw new Error("sandbox run id must be a non-empty string")
  }
  const raw = await request<unknown>(
    "GET",
    `/sandbox/runs/${encodeURIComponent(id)}`,
    null,
    opts,
  )
  return SandboxRunSchema.parse(camelizeRun(raw))
}

/** Stop a running sandbox. Returns true on success. */
export async function stop(
  id: string,
  opts: HttpOptions = {},
): Promise<boolean> {
  if (typeof id !== "string" || id.length === 0) {
    throw new Error("sandbox run id must be a non-empty string")
  }
  const raw = await request<{ stopped?: boolean } | null>(
    "DELETE",
    `/sandbox/runs/${encodeURIComponent(id)}`,
    null,
    opts,
  )
  if (raw === null || raw === undefined) return true
  return Boolean(raw.stopped ?? true)
}

/** Return live pool stats for the active sandbox adapter. */
export async function pool(opts: HttpOptions = {}): Promise<SandboxPoolStats> {
  const raw = await request<unknown>("GET", "/sandbox/pool", null, opts)
  return SandboxPoolStatsSchema.parse(camelizePoolStats(raw))
}

// ---------- helpers ----------

/**
 * Translate the snake_case wire envelope for a single run into the
 * camelCase shape declared by `SandboxRunSchema`. The `command` array is
 * forwarded verbatim (strings, no rewriting).
 */
function camelizeRun(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    id: src.id,
    tenantId: src.tenant_id ?? src.tenantId ?? null,
    workspaceId: src.workspace_id ?? src.workspaceId ?? null,
    adapter: src.adapter,
    image: src.image,
    command: src.command,
    status: src.status,
    exitCode: src.exit_code ?? src.exitCode ?? null,
    durationS: src.duration_s ?? src.durationS ?? null,
    cpuSeconds: src.cpu_seconds ?? src.cpuSeconds ?? null,
    memoryPeakMb: src.memory_peak_mb ?? src.memoryPeakMb ?? null,
    networkBytesIn: src.network_bytes_in ?? src.networkBytesIn ?? null,
    networkBytesOut: src.network_bytes_out ?? src.networkBytesOut ?? null,
    costUsd: src.cost_usd ?? src.costUsd ?? null,
    stdoutUrl: src.stdout_url ?? src.stdoutUrl ?? null,
    stderrUrl: src.stderr_url ?? src.stderrUrl ?? null,
    artifactsUrl: src.artifacts_url ?? src.artifactsUrl ?? null,
    startedAt: src.started_at ?? src.startedAt ?? null,
    endedAt: src.ended_at ?? src.endedAt ?? null,
    createdAt: src.created_at ?? src.createdAt,
  }
}

function camelizeRunList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const runs = Array.isArray(src.runs) ? src.runs.map(camelizeRun) : src.runs
  return {
    runs,
    total: src.total,
    hasMore: src.has_more ?? src.hasMore,
  }
}

function camelizeCapabilities(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    maxTimeoutS: src.max_timeout_s ?? src.maxTimeoutS,
    supportsGpu: src.supports_gpu ?? src.supportsGpu,
    supportsNetwork: src.supports_network ?? src.supportsNetwork,
    supportsMounts: src.supports_mounts ?? src.supportsMounts,
    coldStartMs: src.cold_start_ms ?? src.coldStartMs,
    adapter: src.adapter,
  }
}

function camelizePoolStats(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    adapter: src.adapter,
    warm: src.warm,
    active: src.active,
    queued: src.queued,
    totalRunsToday: src.total_runs_today ?? src.totalRunsToday,
    cpuSecondsToday: src.cpu_seconds_today ?? src.cpuSecondsToday,
    costUsdToday: src.cost_usd_today ?? src.costUsdToday,
    capabilities: camelizeCapabilities(src.capabilities),
  }
}

/** Namespace object — the shape `suite.sandbox` is built from. */
export const sandbox = {
  run,
  list,
  get,
  stop,
  pool,
} as const