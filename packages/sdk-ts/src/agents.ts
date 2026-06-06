// suite.agents.* — invoke AgentField agents via the gateway.
//
// Maps 1:1 to TECH-SPEC.md §4.4. All model calls go through AF; this
// module never reaches a model provider directly.

import { z } from "zod"
import {
  parseSse,
  rawRequest,
  request,
  toCamel,
  type HttpOptions,
  type SseEvent,
} from "./_http.js"

// ---------- shared schemas ----------

const executionStatusSchema = z.enum([
  "queued",
  "running",
  "paused",
  "succeeded",
  "failed",
  "cancelled",
])

const callResultSchema = z.object({
  executionId: z.string(),
  status: executionStatusSchema,
  output: z.unknown().optional(),
  durationMs: z.number().optional(),
  error: z
    .object({
      code: z.string().optional(),
      message: z.string().optional(),
    })
    .optional(),
})
export type CallResult = z.infer<typeof callResultSchema>

const asyncCallResultSchema = z.object({
  executionId: z.string(),
  status: executionStatusSchema,
})
export type AsyncCallResult = z.infer<typeof asyncCallResultSchema>

const executionStatusResponseSchema = z.object({
  executionId: z.string(),
  status: executionStatusSchema,
  output: z.unknown().optional(),
  error: z
    .object({
      code: z.string().optional(),
      message: z.string().optional(),
    })
    .optional(),
  startedAt: z.string().optional(),
  finishedAt: z.string().optional(),
  durationMs: z.number().optional(),
})
export type ExecutionStatus = z.infer<typeof executionStatusResponseSchema>

const pendingApprovalSchema = z.object({
  executionId: z.string(),
  agent: z.string(),
  reason: z.string().optional(),
  requestedAt: z.string().optional(),
  payload: z.unknown().optional(),
})
export type PendingApproval = z.infer<typeof pendingApprovalSchema>

const pendingApprovalsResponseSchema = z.object({
  items: z.array(pendingApprovalSchema),
})

// ---------- input shapes ----------

export interface CallOptions extends HttpOptions {
  /** Server-side execution timeout in seconds (forwarded to AF). */
  timeoutS?: number
  /** Idempotency key — server dedupes within its retention window. */
  idempotencyKey?: string
}

export interface CallAsyncOptions extends CallOptions {
  /** Webhook URL fired when the async execution finishes. */
  webhookUrl?: string
}

export interface ApproveOptions extends HttpOptions {
  byUserId?: string
  note?: string
}

export interface DenyOptions extends HttpOptions {
  byUserId?: string
  reason?: string
}

// ---------- helpers ----------

function encodeAgent(name: string): string {
  // Agent names look like "ns.func". We pass them verbatim except for
  // characters that would break the URL path. AF's gateway accepts dots
  // and dashes natively; everything else gets percent-encoded.
  return encodeURIComponent(name).replace(/%2E/g, ".")
}

function buildCallBody(
  input: unknown,
  extras: Record<string, unknown>,
): Record<string, unknown> {
  // Agent input is forwarded verbatim — agent-defined schemas may carry
  // semantically-meaningful keys (e.g. `result_text` vs `resultText`)
  // and we must not silently rename them. Envelope fields below
  // (timeout_s, webhook_url, etc.) are SDK-defined and translated.
  const body: Record<string, unknown> = { input }
  for (const [k, v] of Object.entries(extras)) {
    if (v !== undefined) body[k] = v
  }
  return body
}

/**
 * Convert only the envelope fields of an execution response to camelCase,
 * leaving user-defined payload fields (`output`, `payload`, `input`)
 * untouched — those follow agent-defined schemas the SDK must not rename.
 */
const OPAQUE_FIELDS = ["output", "payload", "input"] as const

function camelEnvelope(raw: unknown): unknown {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return raw
  const source = raw as Record<string, unknown>
  const stripped: Record<string, unknown> = { ...source }
  const preserved: Record<string, unknown> = {}
  for (const field of OPAQUE_FIELDS) {
    if (field in source) {
      preserved[field] = source[field]
      delete stripped[field]
    }
  }
  const camelRest = toCamel<Record<string, unknown>>(stripped)
  return { ...camelRest, ...preserved }
}

// ---------- public API ----------

/** Sync agent invocation. Returns once AF has produced a result. */
export async function call(
  agent: string,
  input: unknown,
  opts: CallOptions = {},
): Promise<CallResult> {
  const { timeoutS, idempotencyKey, ...http } = opts
  const body = buildCallBody(input, {
    timeout_s: timeoutS,
    idempotency_key: idempotencyKey,
  })
  const raw = await request<unknown>("POST", `/agents/${encodeAgent(agent)}`, body, http)
  return callResultSchema.parse(camelEnvelope(raw))
}

/**
 * Fire-and-track agent invocation. Returns immediately with an execution
 * id; poll with `status()` or stream with `stream()` to observe progress.
 */
export async function callAsync(
  agent: string,
  input: unknown,
  opts: CallAsyncOptions = {},
): Promise<AsyncCallResult> {
  const { timeoutS, idempotencyKey, webhookUrl, ...http } = opts
  const body = buildCallBody(input, {
    timeout_s: timeoutS,
    idempotency_key: idempotencyKey,
    webhook_url: webhookUrl,
  })
  const raw = await request<unknown>(
    "POST",
    `/agents/async/${encodeAgent(agent)}`,
    body,
    http,
  )
  return asyncCallResultSchema.parse(camelEnvelope(raw))
}

/**
 * Stream agent events via SSE. Yields `SseEvent`s; the caller is
 * responsible for interpreting event names. Closes when the server
 * terminates the stream or the supplied AbortSignal fires.
 */
export async function* stream(
  agent: string,
  input: unknown,
  opts: CallOptions = {},
): AsyncIterable<SseEvent> {
  const { timeoutS, idempotencyKey, ...http } = opts
  const body = buildCallBody(input, {
    timeout_s: timeoutS,
    idempotency_key: idempotencyKey,
  })
  const headers = { ...(http.headers ?? {}), accept: "text/event-stream" }
  const response = await rawRequest(
    "POST",
    `/agents/stream/${encodeAgent(agent)}`,
    body,
    { ...http, headers },
  )
  if (response.body === null) return
  yield* parseSse(response.body)
}

/** Fetch the current execution status. */
export async function status(
  executionId: string,
  opts: HttpOptions = {},
): Promise<ExecutionStatus> {
  const raw = await request<unknown>(
    "GET",
    `/executions/${encodeURIComponent(executionId)}`,
    null,
    opts,
  )
  return executionStatusResponseSchema.parse(camelEnvelope(raw))
}

/** Cancel a running execution. */
export async function cancel(
  executionId: string,
  reason?: string,
  opts: HttpOptions = {},
): Promise<void> {
  // The REST surface accepts DELETE with an optional JSON body carrying
  // the reason — fall back to a query parameter on runtimes (Bun, certain
  // Workers) where DELETE bodies are stripped.
  const path = `/executions/${encodeURIComponent(executionId)}`
  await rawRequest("DELETE", path, reason !== undefined ? { reason } : null, opts)
}

/** HITL approval. */
export async function approve(
  executionId: string,
  opts: ApproveOptions = {},
): Promise<void> {
  const { byUserId, note, ...http } = opts
  const body: Record<string, unknown> = {}
  if (byUserId !== undefined) body.by_user_id = byUserId
  if (note !== undefined) body.note = note
  await rawRequest(
    "POST",
    `/executions/${encodeURIComponent(executionId)}/approve`,
    body,
    http,
  )
}

/** HITL denial. */
export async function deny(
  executionId: string,
  opts: DenyOptions = {},
): Promise<void> {
  const { byUserId, reason, ...http } = opts
  const body: Record<string, unknown> = {}
  if (byUserId !== undefined) body.by_user_id = byUserId
  if (reason !== undefined) body.reason = reason
  await rawRequest(
    "POST",
    `/executions/${encodeURIComponent(executionId)}/deny`,
    body,
    http,
  )
}

/** List executions awaiting human approval. */
export async function pendingApprovals(
  opts: HttpOptions = {},
): Promise<PendingApproval[]> {
  const raw = await request<unknown>("GET", "/approvals/pending", null, opts)
  // Apply camelEnvelope to each item to preserve user-defined payloads.
  const rawItems = (raw as { items?: unknown[] }).items ?? []
  const items = rawItems.map(camelEnvelope)
  const parsed = pendingApprovalsResponseSchema.parse({ items })
  return parsed.items
}

/** Namespace object — the shape `suite.agents` is built from. */
export const agents = {
  call,
  callAsync,
  stream,
  status,
  cancel,
  approve,
  deny,
  pendingApprovals,
} as const
