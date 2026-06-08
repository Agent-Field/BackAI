// SPDX-License-Identifier: Apache-2.0

// suite.shipwright.* — autonomous coding-agent task factory.
//
// AF Stack stores task + patch metadata. AgentField owns the actual coding
// agent run, harness calls, live logs, spans, traces, and memory.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

export const ShipwrightStatusSchema = z.enum([
  "queued",
  "running",
  "succeeded",
  "failed",
  "cancelled",
])
export type ShipwrightStatus = z.infer<typeof ShipwrightStatusSchema>

export const ShipwrightTaskSchema = z.object({
  id: z.string(),
  tenantId: z.string(),
  userId: z.string().nullable(),
  title: z.string(),
  description: z.string(),
  repoUrl: z.string(),
  status: ShipwrightStatusSchema,
  runId: z.string().nullable(),
  createdAt: z.string(),
  updatedAt: z.string(),
})
export type ShipwrightTask = z.infer<typeof ShipwrightTaskSchema>

export const ShipwrightPatchSchema = z.object({
  taskId: z.string(),
  ref: z.string(),
  summary: z.string(),
  diffUrl: z.string().nullable(),
  createdAt: z.string(),
})
export type ShipwrightPatch = z.infer<typeof ShipwrightPatchSchema>

export const ShipwrightTaskResponseSchema = z.object({
  task: ShipwrightTaskSchema,
  patches: z.array(ShipwrightPatchSchema).optional(),
  agentCall: z.string().optional(),
  agentFieldUrl: z.string().optional(),
  detailsUrl: z.string().optional(),
})
export type ShipwrightTaskResponse = z.infer<typeof ShipwrightTaskResponseSchema>

export const ShipwrightTaskListSchema = z.object({
  tasks: z.array(ShipwrightTaskSchema),
  total: z.number(),
  hasMore: z.boolean(),
})
export type ShipwrightTaskList = z.infer<typeof ShipwrightTaskListSchema>

export interface CreateShipwrightTaskInput extends HttpOptions {
  title: string
  description: string
  repoUrl: string
  harnessProvider?: "codex" | "claude-code" | "gemini" | "opencode" | string
  model?: string
}

export interface ListShipwrightTasksOptions extends HttpOptions {
  status?: ShipwrightStatus | string
  limit?: number
  offset?: number
}

export interface CompleteShipwrightTaskInput extends HttpOptions {
  status?: Extract<ShipwrightStatus, "succeeded" | "failed" | "cancelled">
  ref?: string
  summary?: string
  diffUrl?: string
}

export async function create(
  input: CreateShipwrightTaskInput,
): Promise<ShipwrightTaskResponse> {
  const { title, description, repoUrl, harnessProvider, model, ...http } = input
  if (title.trim() === "") throw new Error("title must be a non-empty string")
  if (description.trim() === "") throw new Error("description must be a non-empty string")
  if (repoUrl.trim() === "") throw new Error("repoUrl must be a non-empty string")
  const raw = await request<unknown>(
    "POST",
    "/shipwright/tasks",
    {
      title,
      description,
      repo_url: repoUrl,
      harness_provider: harnessProvider,
      model,
    },
    http,
  )
  return ShipwrightTaskResponseSchema.parse(camelizeTaskResponse(raw))
}

export async function get(id: string, opts: HttpOptions = {}): Promise<ShipwrightTaskResponse> {
  if (id.trim() === "") throw new Error("task id must be a non-empty string")
  const raw = await request<unknown>(
    "GET",
    `/shipwright/tasks/${encodeURIComponent(id)}`,
    null,
    opts,
  )
  return ShipwrightTaskResponseSchema.parse(camelizeTaskResponse(raw))
}

export async function list(
  opts: ListShipwrightTasksOptions = {},
): Promise<ShipwrightTaskList> {
  const { status, limit = 25, offset = 0, ...http } = opts
  const qs = new URLSearchParams()
  if (status !== undefined) qs.set("status", status)
  qs.set("limit", String(limit))
  qs.set("offset", String(offset))
  const raw = await request<unknown>(
    "GET",
    `/shipwright/tasks?${qs.toString()}`,
    null,
    http,
  )
  return ShipwrightTaskListSchema.parse(camelizeTaskList(raw))
}

export async function complete(
  id: string,
  input: CompleteShipwrightTaskInput = {},
): Promise<ShipwrightTaskResponse> {
  if (id.trim() === "") throw new Error("task id must be a non-empty string")
  const { status, ref, summary, diffUrl, ...http } = input
  const raw = await request<unknown>(
    "POST",
    `/shipwright/tasks/${encodeURIComponent(id)}/complete`,
    {
      status,
      ref,
      summary,
      diff_url: diffUrl,
    },
    http,
  )
  return ShipwrightTaskResponseSchema.parse(camelizeTaskResponse(raw))
}

function camelizeTaskResponse(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    task: camelizeTask(src.task),
    patches: Array.isArray(src.patches) ? src.patches.map(camelizePatch) : src.patches,
    agentCall: src.agent_call ?? src.agentCall,
    agentFieldUrl: src.agentfield_url ?? src.agentFieldUrl,
    detailsUrl: src.details_url ?? src.detailsUrl,
  }
}

function camelizeTaskList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    tasks: Array.isArray(src.tasks) ? src.tasks.map(camelizeTask) : src.tasks,
    total: src.total,
    hasMore: src.has_more ?? src.hasMore,
  }
}

function camelizeTask(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    id: src.id,
    tenantId: src.tenant_id ?? src.tenantId,
    userId: src.user_id ?? src.userId ?? null,
    title: src.title,
    description: src.description,
    repoUrl: src.repo_url ?? src.repoUrl,
    status: src.status,
    runId: src.run_id ?? src.runId ?? null,
    createdAt: src.created_at ?? src.createdAt,
    updatedAt: src.updated_at ?? src.updatedAt,
  }
}

function camelizePatch(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    taskId: src.task_id ?? src.taskId,
    ref: src.ref,
    summary: src.summary,
    diffUrl: src.diff_url ?? src.diffUrl ?? null,
    createdAt: src.created_at ?? src.createdAt,
  }
}

export const shipwright = {
  create,
  get,
  list,
  complete,
} as const
