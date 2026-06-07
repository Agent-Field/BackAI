// suite.notifications.* — outbox-style notifications.
//
// Endpoint paths and JSON shapes are the canonical contract from
// `apps/dashboard/src/lib/api.ts` (the Phase 10.1 Notifications
// section). Schemas below mirror the zod schemas there; if the
// dashboard contract moves, move these together.
//
// Public API stays camelCase; the wire stays snake_case. The dashboard
// schemas use snake_case already, so we translate on input/output here
// (the `from` -> `from_` mapping in particular avoids the reserved-word
// foot-gun some JS frameworks hit).

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

// ---------- shared schemas (mirror lib/api.ts, camelCase) ----------

export const NotificationKindSchema = z.enum(["email", "sms", "push", "log"])
export type NotificationKind = z.infer<typeof NotificationKindSchema>

export const NotificationStatusSchema = z.enum([
  "queued",
  "sending",
  "sent",
  "failed",
  "skipped",
])
export type NotificationStatus = z.infer<typeof NotificationStatusSchema>

export const NotificationSchema = z.object({
  id: z.string(),
  tenantId: z.string().nullable(),
  kind: NotificationKindSchema,
  adapter: z.string().nullable(),
  template: z.string(),
  to: z.string(),
  from: z.string().nullable(),
  subject: z.string().nullable(),
  data: z.record(z.string(), z.unknown()),
  status: NotificationStatusSchema,
  providerMessageId: z.string().nullable(),
  attempts: z.number(),
  lastError: z.string().nullable(),
  scheduledAt: z.string(),
  sentAt: z.string().nullable(),
  createdAt: z.string(),
})
export type Notification = z.infer<typeof NotificationSchema>

export const NotificationListSchema = z.object({
  notifications: z.array(NotificationSchema),
  total: z.number(),
  hasMore: z.boolean(),
})
export type NotificationList = z.infer<typeof NotificationListSchema>

export const AdapterCountSchema = z.object({
  adapter: z.string(),
  count: z.number(),
})
export type AdapterCount = z.infer<typeof AdapterCountSchema>

export const NotificationStatsSchema = z.object({
  byStatus: z.record(z.string(), z.number()),
  byAdapter: z.array(AdapterCountSchema),
  sentToday: z.number(),
  failedToday: z.number(),
})
export type NotificationStats = z.infer<typeof NotificationStatsSchema>

// ---------- public API ----------

const ALLOWED_KINDS: readonly NotificationKind[] = ["email", "sms", "push", "log"]

export interface SendNotificationOptions extends HttpOptions {
  /** Recipient address. Email for kind=email, phone for sms, etc. */
  to: string
  /** Template slug. The runtime renders against `data`. Default: "default". */
  template?: string
  /** Channel. Default: "email". */
  kind?: NotificationKind | string
  /** Subject (email/push only). */
  subject?: string
  /** From address (overrides the adapter default). */
  from?: string
  /** Free-form payload passed into the template renderer. */
  data?: Record<string, unknown>
  /** ISO 8601 timestamp. Omit to send immediately. */
  scheduledAt?: string
}

export interface EmailNotificationOptions extends HttpOptions {
  /** Plain-text body. Folded into `data.body` so the default renderer can use it. */
  body?: string
  /** Template slug. Default: "default". */
  template?: string
  /** Free-form payload passed into the template renderer. */
  data?: Record<string, unknown>
  /** From address (overrides the adapter default). */
  from?: string
  /** ISO 8601 timestamp. Omit to send immediately. */
  scheduledAt?: string
}

export interface ListNotificationsOptions extends HttpOptions {
  tenant?: string
  status?: NotificationStatus | string
  kind?: NotificationKind | string
  limit?: number
  offset?: number
}

/** Queue a notification for delivery. */
export async function send(opts: SendNotificationOptions): Promise<Notification> {
  if (typeof opts.to !== "string" || opts.to.length === 0) {
    throw new Error("`to` must be a non-empty string")
  }
  const {
    to,
    template = "default",
    kind = "email",
    subject,
    from,
    data,
    scheduledAt,
    ...http
  } = opts
  if (typeof template !== "string" || template.length === 0) {
    throw new Error("`template` must be a non-empty string")
  }
  if (!(ALLOWED_KINDS as readonly string[]).includes(kind)) {
    throw new Error(
      `kind must be one of: ${ALLOWED_KINDS.join(", ")}; got ${JSON.stringify(kind)}`,
    )
  }
  const body: Record<string, unknown> = {
    to,
    template,
    kind,
  }
  if (subject !== undefined) body.subject = subject
  if (from !== undefined) body.from = from
  if (data !== undefined) body.data = { ...data }
  if (scheduledAt !== undefined) body.scheduled_at = scheduledAt

  const raw = await request<unknown>("POST", "/notifications", body, http)
  return NotificationSchema.parse(camelizeNotification(raw))
}

/**
 * Convenience for `send({ kind: "email", ... })`.
 *
 * `body` is folded into `data.body` so the default template renderer
 * (which substitutes `{{key}}` tokens against `data`) can produce a
 * plain-text email without a real template registry.
 */
export async function email(
  to: string,
  subject: string,
  opts: EmailNotificationOptions = {},
): Promise<Notification> {
  if (typeof subject !== "string" || subject.length === 0) {
    throw new Error("`subject` must be a non-empty string")
  }
  const { body, template, data, from, scheduledAt, ...http } = opts
  const payloadData: Record<string, unknown> = { ...(data ?? {}) }
  if (body !== undefined) payloadData.body = body
  return send({
    to,
    subject,
    kind: "email",
    template: template ?? "default",
    data: payloadData,
    from,
    scheduledAt,
    ...http,
  })
}

/** List notifications with optional filters. */
export async function list(
  opts: ListNotificationsOptions = {},
): Promise<NotificationList> {
  const { tenant, status, kind, limit = 50, offset = 0, ...http } = opts
  if (limit <= 0) throw new Error("limit must be a positive integer")
  if (offset < 0) throw new Error("offset must be non-negative")
  const qs = new URLSearchParams()
  if (tenant !== undefined) qs.set("tenant", tenant)
  if (status !== undefined) qs.set("status", status)
  if (kind !== undefined) qs.set("kind", kind)
  qs.set("limit", String(limit))
  qs.set("offset", String(offset))
  const raw = await request<unknown>(
    "GET",
    `/notifications?${qs.toString()}`,
    null,
    http,
  )
  return NotificationListSchema.parse(camelizeNotificationList(raw))
}

/** Fetch a single notification by id. */
export async function get(
  id: string,
  opts: HttpOptions = {},
): Promise<Notification> {
  if (typeof id !== "string" || id.length === 0) {
    throw new Error("notification id must be a non-empty string")
  }
  const raw = await request<unknown>(
    "GET",
    `/notifications/${encodeURIComponent(id)}`,
    null,
    opts,
  )
  return NotificationSchema.parse(camelizeNotification(raw))
}

/** Return KPI aggregates for the dashboard's notifications tab. */
export async function stats(
  opts: HttpOptions = {},
): Promise<NotificationStats> {
  const raw = await request<unknown>("GET", "/notifications/stats", null, opts)
  return NotificationStatsSchema.parse(camelizeStats(raw))
}

// ---------- helpers ----------

/**
 * Translate the snake_case wire envelope for a single notification into
 * the camelCase shape declared by `NotificationSchema`. `data` is
 * forwarded verbatim (it's a free-form payload).
 */
function camelizeNotification(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    id: src.id,
    tenantId: src.tenant_id ?? src.tenantId ?? null,
    kind: src.kind,
    adapter: src.adapter ?? null,
    template: src.template,
    to: src.to,
    from: src.from ?? null,
    subject: src.subject ?? null,
    data: src.data ?? {},
    status: src.status,
    providerMessageId:
      src.provider_message_id ?? src.providerMessageId ?? null,
    attempts: src.attempts,
    lastError: src.last_error ?? src.lastError ?? null,
    scheduledAt: src.scheduled_at ?? src.scheduledAt,
    sentAt: src.sent_at ?? src.sentAt ?? null,
    createdAt: src.created_at ?? src.createdAt,
  }
}

function camelizeNotificationList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const notifications = Array.isArray(src.notifications)
    ? src.notifications.map(camelizeNotification)
    : src.notifications
  return {
    notifications,
    total: src.total,
    hasMore: src.has_more ?? src.hasMore,
  }
}

function camelizeStats(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    byStatus: src.by_status ?? src.byStatus ?? {},
    byAdapter: src.by_adapter ?? src.byAdapter ?? [],
    sentToday: src.sent_today ?? src.sentToday ?? 0,
    failedToday: src.failed_today ?? src.failedToday ?? 0,
  }
}

/** Namespace object — the shape `suite.notifications` is built from. */
export const notifications = {
  send,
  email,
  list,
  get,
  stats,
} as const
