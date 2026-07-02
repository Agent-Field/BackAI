// SPDX-License-Identifier: Apache-2.0

// Minimal API client for the customer-app.
//
// We deliberately mirror only the SCHEMAS the customer-app actually uses
// (cost, cost events, billing). The dashboard's full schema set lives in
// apps/dashboard/src/lib/api.ts; if you need a new endpoint here, copy
// the schema in and don't try to import across apps — the Docker build
// only sees this directory.

import { z } from "zod"

const isServer = typeof window === "undefined"

function baseUrl(): string {
  if (isServer) {
    return process.env.RUNTIME_URL ?? "http://localhost:8080"
  }
  // Browser: same-origin proxy via /api/v1/[...path]/route.ts.
  return ""
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details?: unknown
  constructor(
    message: string,
    status: number,
    code: string,
    details?: unknown,
  ) {
    super(message)
    this.status = status
    this.code = code
    this.details = details
  }
}

// ─── Schemas (mirror of the dashboard's schemas) ──────────────────────

const CostPointSchema = z.object({
  date: z.string(),
  cost_usd: z.number(),
})

export const CostSummarySchema = z.object({
  period_total_usd: z.number(),
  previous_total_usd: z.number(),
  budget_usd: z.number().nullable(),
  forecast_usd: z.number(),
  by_day: z.array(CostPointSchema),
  by_model: z.array(z.object({ model: z.string(), cost_usd: z.number() })),
  by_agent: z.array(z.object({ agent: z.string(), cost_usd: z.number() })),
  by_tenant: z.array(
    z.object({
      tenant_id: z.string(),
      tenant_name: z.string().nullable(),
      cost_usd: z.number(),
    }),
  ),
})
export type CostSummary = z.infer<typeof CostSummarySchema>

export const CostEventSchema = z.object({
  id: z.string(),
  request_id: z.string().nullable().optional(),
  tenant_id: z.string().nullable(),
  api_key_id: z.string().nullable(),
  model: z.string(),
  provider: z.string(),
  agent: z.string().nullable(),
  prompt_tokens: z.number(),
  completion_tokens: z.number(),
  total_tokens: z.number(),
  cost_usd: z.number(),
  cached: z.boolean(),
  latency_ms: z.number(),
  occurred_at: z.string(),
})
export type CostEvent = z.infer<typeof CostEventSchema>

export const CostEventListSchema = z.object({
  events: z.array(CostEventSchema),
  total: z.number(),
  has_more: z.boolean(),
})
export type CostEventList = z.infer<typeof CostEventListSchema>

export const BillingCustomerSchema = z.object({
  tenant_id: z.string(),
  stripe_customer_id: z.string().nullable(),
  email: z.string().nullable(),
  plan: z.string(),
  trial_ends_at: z.string().nullable(),
  current_period_end: z.string().nullable(),
  subscription_status: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
})
export type BillingCustomer = z.infer<typeof BillingCustomerSchema>

export const UsageMeterSchema = z.object({
  meter: z.string(),
  tenant_id: z.string(),
  period_start: z.string(),
  period_end: z.string(),
  quantity: z.number(),
  cost_usd: z.number().nullable(),
  stripe_meter_id: z.string().nullable(),
  last_synced_at: z.string().nullable(),
})
export type UsageMeter = z.infer<typeof UsageMeterSchema>

export const UsageMeterListSchema = z.object({
  meters: z.array(UsageMeterSchema),
  total_cost_usd: z.number(),
})
export type UsageMeterList = z.infer<typeof UsageMeterListSchema>

export const PortalLinkSchema = z.object({
  url: z.string(),
  expires_at: z.string(),
})
export type PortalLink = z.infer<typeof PortalLinkSchema>

// ─── Transport ────────────────────────────────────────────────────────

type RequestInitWithJson = Omit<RequestInit, "body"> & { json?: unknown }

async function serverCookieHeader(): Promise<string | undefined> {
  if (!isServer) return undefined
  try {
    const { cookies } = await import("next/headers")
    const store = await cookies()
    // Drop the operator dashboard's cookies (shared host) — customer
    // calls must resolve the customer session only.
    return store
      .getAll()
      .filter((c) => !c.name.includes("backai-operator"))
      .map((c) => `${c.name}=${c.value}`)
      .join("; ")
  } catch {
    return undefined
  }
}

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
  const cookieHeader = await serverCookieHeader()
  if (cookieHeader && !headers.has("cookie")) {
    headers.set("cookie", cookieHeader)
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
      throw new ApiError(
        `Non-JSON response from ${path}`,
        res.status,
        "BAD_RESPONSE",
      )
    }
  }
  if (!res.ok) {
    const envelope = parsed as {
      error?: { code?: string; message?: string; details?: unknown }
    }
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

// ─── Methods ──────────────────────────────────────────────────────────

export const api = {
  cost: (params?: { from?: string; to?: string; tenant?: string }) => {
    const qs = new URLSearchParams()
    if (params?.from) qs.set("from", params.from)
    if (params?.to) qs.set("to", params.to)
    if (params?.tenant) qs.set("tenant", params.tenant)
    const q = qs.toString()
    return request(
      `/api/v1/cost${q ? "?" + q : ""}`,
      undefined,
      CostSummarySchema,
    )
  },
  costEvents: (params?: {
    tenant?: string
    request_id?: string
    limit?: number
    offset?: number
  }) => {
    const qs = new URLSearchParams()
    if (params?.tenant) qs.set("tenant", params.tenant)
    if (params?.request_id) qs.set("request_id", params.request_id)
    if (params?.limit !== undefined) qs.set("limit", String(params.limit))
    if (params?.offset !== undefined) qs.set("offset", String(params.offset))
    const q = qs.toString()
    return request(
      `/api/v1/cost/events${q ? "?" + q : ""}`,
      undefined,
      CostEventListSchema,
    )
  },
  billing: {
    customer: (tenantId: string) =>
      request(
        `/api/v1/billing/customers/${encodeURIComponent(tenantId)}`,
        undefined,
        BillingCustomerSchema,
      ),
    meters: (params?: {
      tenant?: string
      period_start?: string
      bucket?: "month" | "day"
    }) => {
      const qs = new URLSearchParams()
      if (params?.tenant) qs.set("tenant", params.tenant)
      if (params?.period_start) qs.set("period_start", params.period_start)
      if (params?.bucket) qs.set("bucket", params.bucket)
      const q = qs.toString()
      return request(
        `/api/v1/billing/meters${q ? "?" + q : ""}`,
        undefined,
        UsageMeterListSchema,
      )
    },
    portalLink: (tenantId: string) =>
      request(
        `/api/v1/billing/customers/${encodeURIComponent(tenantId)}/portal`,
        { method: "POST" },
        PortalLinkSchema,
      ),
  },
}
