// suite.cost.* — cost event log browsing.
//
// Endpoint (mirroring `apps/dashboard/src/lib/api.ts`):
//
//   GET /api/v1/cost/events?tenant=&model=&from=&to=&limit=&offset=
//
// Each `CostEvent` is one billable LLM call written by the cost recorder
// (Phase 7.2). Schemas exposed here are camelCase; the wire stays
// snake_case and is translated via `toCamel`.

import { z } from "zod"
import { request, toCamel, type HttpOptions } from "./_http.js"

// ---------- shared schemas (mirror lib/api.ts, camelCase) ----------

export const CostEventSchema = z.object({
  id: z.string(),
  tenantId: z.string().nullable(),
  apiKeyId: z.string().nullable(),
  model: z.string(),
  provider: z.string(),
  agent: z.string().nullable(),
  promptTokens: z.number(),
  completionTokens: z.number(),
  totalTokens: z.number(),
  costUsd: z.number(),
  cached: z.boolean(),
  latencyMs: z.number(),
  occurredAt: z.string(),
})
export type CostEvent = z.infer<typeof CostEventSchema>

export const CostEventListSchema = z.object({
  events: z.array(CostEventSchema),
  total: z.number(),
  hasMore: z.boolean(),
})
export type CostEventList = z.infer<typeof CostEventListSchema>

// ---------- public API ----------

export interface ListCostEventsOptions extends HttpOptions {
  tenant?: string
  model?: string
  /** RFC3339 timestamp or Date. */
  from?: string | Date
  /** RFC3339 timestamp or Date. */
  to?: string | Date
  limit?: number
  offset?: number
}

/** List cost events with optional filters. */
export async function events(
  opts: ListCostEventsOptions = {},
): Promise<CostEventList> {
  const {
    tenant,
    model,
    from,
    to,
    limit = 100,
    offset = 0,
    ...http
  } = opts
  if (limit <= 0) throw new Error("limit must be a positive integer")
  if (offset < 0) throw new Error("offset must be non-negative")
  const qs = new URLSearchParams()
  if (tenant !== undefined && tenant !== "") qs.set("tenant", tenant)
  if (model !== undefined && model !== "") qs.set("model", model)
  if (from !== undefined) qs.set("from", toIso(from))
  if (to !== undefined) qs.set("to", toIso(to))
  qs.set("limit", String(limit))
  qs.set("offset", String(offset))
  const raw = await request<unknown>(
    "GET",
    `/cost/events?${qs.toString()}`,
    null,
    http,
  )
  return CostEventListSchema.parse(toCamel(raw))
}

function toIso(value: string | Date): string {
  return value instanceof Date ? value.toISOString() : value
}

/** Namespace object — the shape `suite.cost` is built from. */
export const cost = {
  events,
} as const
