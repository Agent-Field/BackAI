// suite.webhooks.* — webhook outbox + unified delivery feed operations.
//
// Endpoint paths and JSON shapes are the canonical contract from
// `apps/dashboard/src/lib/api.ts` (the Phase 10.2 + 10.3 Webhooks
// section). Schemas below mirror the zod schemas there; if the dashboard
// contract moves, move these together.
//
// Public API stays camelCase; the wire stays snake_case. We translate at
// the boundary with `camelize…` helpers — the `body` payload (which is
// opaque to us) is forwarded verbatim.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

// ---------- shared schemas (mirror lib/api.ts, camelCase) ----------

export const WebhookDirectionSchema = z.enum(["inbound", "outbound"])
export type WebhookDirection = z.infer<typeof WebhookDirectionSchema>

export const WebhookStatusSchema = z.enum([
  "queued",
  "delivering",
  "succeeded",
  "failed",
  "dropped_duplicate",
  "dropped_invalid_signature",
])
export type WebhookStatus = z.infer<typeof WebhookStatusSchema>

export const WebhookDeliverySchema = z.object({
  id: z.string(),
  endpointId: z.string().nullable(),
  direction: WebhookDirectionSchema,
  destination: z.string(),
  eventType: z.string(),
  status: WebhookStatusSchema,
  attempts: z.number(),
  responseStatus: z.number().nullable(),
  responseMs: z.number().nullable(),
  bodyPreview: z.string().nullable(),
  bodyStorageUrl: z.string().nullable(),
  lastError: z.string().nullable(),
  scheduledAt: z.string(),
  deliveredAt: z.string().nullable(),
  createdAt: z.string(),
})
export type WebhookDelivery = z.infer<typeof WebhookDeliverySchema>

export const WebhookDeliveryListSchema = z.object({
  deliveries: z.array(WebhookDeliverySchema),
  total: z.number(),
  hasMore: z.boolean(),
})
export type WebhookDeliveryList = z.infer<typeof WebhookDeliveryListSchema>

// ---------- public API ----------

const ALLOWED_DIRECTIONS: readonly WebhookDirection[] = ["inbound", "outbound"]

export interface SendWebhookOptions extends HttpOptions {
  /** Extra headers merged onto the outbound POST. */
  headers?: Record<string, string>
  /** When `true`, POST synchronously before returning. Default `false`
   *  (the row sits at `queued` and the background worker drains it). */
  immediate?: boolean
}

export interface ListWebhookDeliveriesOptions extends HttpOptions {
  direction?: WebhookDirection | string
  endpointId?: string
  tenant?: string
  status?: WebhookStatus | string
  limit?: number
  offset?: number
}

/** Enqueue an outbound webhook delivery. Returns the persisted row. */
export async function send(
  url: string,
  eventType: string,
  body: unknown,
  opts: SendWebhookOptions = {},
): Promise<WebhookDelivery> {
  if (typeof url !== "string" || url.length === 0) {
    throw new Error("webhook url must be a non-empty string")
  }
  if (typeof eventType !== "string" || eventType.length === 0) {
    throw new Error("webhook event_type must be a non-empty string")
  }

  const { headers, immediate = false, ...http } = opts

  const payload: Record<string, unknown> = {
    url,
    event_type: eventType,
    body,
    immediate: Boolean(immediate),
  }
  if (headers !== undefined) {
    payload.headers = { ...headers }
  }

  const raw = await request<unknown>("POST", "/webhooks/send", payload, http)
  return WebhookDeliverySchema.parse(camelizeDelivery(raw))
}

/** List webhook deliveries with optional filters. */
export async function list(
  opts: ListWebhookDeliveriesOptions = {},
): Promise<WebhookDeliveryList> {
  const { direction, endpointId, tenant, status, limit = 50, offset = 0, ...http } = opts
  if (limit <= 0) throw new Error("limit must be a positive integer")
  if (offset < 0) throw new Error("offset must be non-negative")
  if (
    direction !== undefined &&
    !(ALLOWED_DIRECTIONS as readonly string[]).includes(direction)
  ) {
    throw new Error(
      `direction must be one of: ${ALLOWED_DIRECTIONS.join(", ")}; got ${JSON.stringify(direction)}`,
    )
  }

  const qs = new URLSearchParams()
  if (direction !== undefined) qs.set("direction", direction)
  if (endpointId !== undefined) qs.set("endpoint_id", endpointId)
  if (tenant !== undefined) qs.set("tenant", tenant)
  if (status !== undefined) qs.set("status", status)
  qs.set("limit", String(limit))
  qs.set("offset", String(offset))
  const raw = await request<unknown>(
    "GET",
    `/webhooks/deliveries?${qs.toString()}`,
    null,
    http,
  )
  return WebhookDeliveryListSchema.parse(camelizeList(raw))
}

/** Fetch a single delivery row. */
export async function get(
  id: string,
  opts: HttpOptions = {},
): Promise<WebhookDelivery> {
  if (typeof id !== "string" || id.length === 0) {
    throw new Error("webhook delivery id must be a non-empty string")
  }
  const raw = await request<unknown>(
    "GET",
    `/webhooks/deliveries/${encodeURIComponent(id)}`,
    null,
    opts,
  )
  return WebhookDeliverySchema.parse(camelizeDelivery(raw))
}

/** Re-queue a delivery for the retry worker. Returns the refreshed row. */
export async function retry(
  id: string,
  opts: HttpOptions = {},
): Promise<WebhookDelivery> {
  if (typeof id !== "string" || id.length === 0) {
    throw new Error("webhook delivery id must be a non-empty string")
  }
  const raw = await request<unknown>(
    "POST",
    `/webhooks/deliveries/${encodeURIComponent(id)}/retry`,
    null,
    opts,
  )
  return WebhookDeliverySchema.parse(camelizeDelivery(raw))
}

// ---------- helpers ----------

/**
 * Translate the snake_case wire envelope for a single delivery into the
 * camelCase shape declared by `WebhookDeliverySchema`. We fall through
 * the camelCase key when set so the helper is idempotent on already-
 * translated objects (used by the unit tests).
 */
function camelizeDelivery(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    id: src.id,
    endpointId: src.endpoint_id ?? src.endpointId ?? null,
    direction: src.direction,
    destination: src.destination,
    eventType: src.event_type ?? src.eventType,
    status: src.status,
    attempts: src.attempts,
    responseStatus: src.response_status ?? src.responseStatus ?? null,
    responseMs: src.response_ms ?? src.responseMs ?? null,
    bodyPreview: src.body_preview ?? src.bodyPreview ?? null,
    bodyStorageUrl: src.body_storage_url ?? src.bodyStorageUrl ?? null,
    lastError: src.last_error ?? src.lastError ?? null,
    scheduledAt: src.scheduled_at ?? src.scheduledAt,
    deliveredAt: src.delivered_at ?? src.deliveredAt ?? null,
    createdAt: src.created_at ?? src.createdAt,
  }
}

function camelizeList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const deliveries = Array.isArray(src.deliveries)
    ? src.deliveries.map(camelizeDelivery)
    : src.deliveries
  return {
    deliveries,
    total: src.total,
    hasMore: src.has_more ?? src.hasMore,
  }
}

/** Namespace object — the shape `suite.webhooks` is built from. */
export const webhooks = {
  send,
  list,
  get,
  retry,
} as const
