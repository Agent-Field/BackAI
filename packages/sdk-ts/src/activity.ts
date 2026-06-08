// SPDX-License-Identifier: Apache-2.0

// suite.activity.* — tenant-scoped customer/product activity log.
//
// This is not the admin audit log and not AgentField run state. Use it
// for app events like uploads, searches, onboarding steps, purchases,
// and domain actions.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

export const ActivityActorTypeSchema = z.enum([
  "user",
  "api_key",
  "system",
  "anonymous",
])
export type ActivityActorType = z.infer<typeof ActivityActorTypeSchema>

export const ActivityEntrySchema = z.object({
  id: z.string(),
  tenantId: z.string(),
  userId: z.string().nullable(),
  apiKeyId: z.string().nullable(),
  actorType: ActivityActorTypeSchema,
  action: z.string(),
  resourceType: z.string().nullable(),
  resourceId: z.string().nullable(),
  metadata: z.record(z.string(), z.unknown()),
  ip: z.string().nullable(),
  userAgent: z.string().nullable(),
  occurredAt: z.string(),
})
export type ActivityEntry = z.infer<typeof ActivityEntrySchema>

export const ActivityListSchema = z.object({
  entries: z.array(ActivityEntrySchema),
  total: z.number(),
  hasMore: z.boolean(),
})
export type ActivityList = z.infer<typeof ActivityListSchema>

export interface LogActivityOptions extends HttpOptions {
  userId?: string
  actorType?: ActivityActorType
  resourceType?: string
  resourceId?: string
  metadata?: Record<string, unknown>
  occurredAt?: string | Date
}

export interface ListActivityOptions extends HttpOptions {
  userId?: string
  action?: string
  resourceType?: string
  resourceId?: string
  from?: string | Date
  to?: string | Date
  limit?: number
  offset?: number
}

export async function log(
  action: string,
  opts: LogActivityOptions = {},
): Promise<ActivityEntry> {
  if (typeof action !== "string" || action.trim() === "") {
    throw new Error("activity action must be a non-empty string")
  }
  const {
    userId,
    actorType,
    resourceType,
    resourceId,
    metadata,
    occurredAt,
    ...http
  } = opts
  if (actorType !== undefined) validateActorType(actorType)
  if (resourceId !== undefined && resourceId !== "" && (resourceType === undefined || resourceType === "")) {
    throw new Error("resourceType is required when resourceId is set")
  }

  const body: Record<string, unknown> = { action }
  if (userId !== undefined && userId !== "") body.user_id = userId
  if (actorType !== undefined) body.actor_type = actorType
  if (resourceType !== undefined && resourceType !== "") body.resource_type = resourceType
  if (resourceId !== undefined && resourceId !== "") body.resource_id = resourceId
  if (metadata !== undefined) body.metadata = metadata
  if (occurredAt !== undefined) body.occurred_at = toIso(occurredAt)

  const raw = await request<unknown>("POST", "/activity", body, http)
  return ActivityEntrySchema.parse(camelizeEntry(raw))
}

export async function list(
  opts: ListActivityOptions = {},
): Promise<ActivityList> {
  const {
    userId,
    action,
    resourceType,
    resourceId,
    from,
    to,
    limit = 100,
    offset = 0,
    ...http
  } = opts
  if (limit <= 0) throw new Error("limit must be a positive integer")
  if (offset < 0) throw new Error("offset must be non-negative")

  const qs = new URLSearchParams()
  if (userId !== undefined && userId !== "") qs.set("user_id", userId)
  if (action !== undefined && action !== "") qs.set("action", action)
  if (resourceType !== undefined && resourceType !== "") qs.set("resource_type", resourceType)
  if (resourceId !== undefined && resourceId !== "") qs.set("resource_id", resourceId)
  if (from !== undefined) qs.set("from", toIso(from))
  if (to !== undefined) qs.set("to", toIso(to))
  qs.set("limit", String(limit))
  qs.set("offset", String(offset))

  const raw = await request<unknown>("GET", `/activity?${qs.toString()}`, null, http)
  return ActivityListSchema.parse(camelizeList(raw))
}

function validateActorType(actorType: string): void {
  if (!["user", "api_key", "system", "anonymous"].includes(actorType)) {
    throw new Error(
      `actorType must be user, api_key, system, or anonymous; got ${JSON.stringify(actorType)}`,
    )
  }
}

function toIso(value: string | Date): string {
  return value instanceof Date ? value.toISOString() : value
}

function camelizeEntry(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    id: src.id,
    tenantId: src.tenant_id ?? src.tenantId,
    userId: src.user_id ?? src.userId ?? null,
    apiKeyId: src.api_key_id ?? src.apiKeyId ?? null,
    actorType: src.actor_type ?? src.actorType,
    action: src.action,
    resourceType: src.resource_type ?? src.resourceType ?? null,
    resourceId: src.resource_id ?? src.resourceId ?? null,
    metadata: src.metadata ?? {},
    ip: src.ip ?? null,
    userAgent: src.user_agent ?? src.userAgent ?? null,
    occurredAt: src.occurred_at ?? src.occurredAt,
  }
}

function camelizeList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const entries = Array.isArray(src.entries)
    ? src.entries.map(camelizeEntry)
    : src.entries
  return {
    entries,
    total: src.total,
    hasMore: src.has_more ?? src.hasMore,
  }
}

export const activity = {
  log,
  list,
}
