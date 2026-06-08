// SPDX-License-Identifier: Apache-2.0

// suite.flags.* — runtime feature flags.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

export const FeatureFlagSchema = z.object({
  key: z.string(),
  label: z.string(),
  description: z.string(),
  enabled: z.boolean(),
  source: z.string(),
  metadata: z.record(z.string(), z.unknown()),
  updatedAt: z.string().nullable(),
})
export type FeatureFlag = z.infer<typeof FeatureFlagSchema>

export const FeatureFlagListSchema = z.object({
  flags: z.array(FeatureFlagSchema),
})
export type FeatureFlagList = z.infer<typeof FeatureFlagListSchema>

export interface SetFeatureFlagOptions extends HttpOptions {
  label?: string
  description?: string
  metadata?: Record<string, unknown>
}

export async function list(opts: HttpOptions = {}): Promise<FeatureFlagList> {
  const raw = await request<unknown>("GET", "/config/flags", null, opts)
  return FeatureFlagListSchema.parse(camelizeList(raw))
}

export async function set(
  key: string,
  enabled: boolean,
  opts: SetFeatureFlagOptions = {},
): Promise<FeatureFlag> {
  if (typeof key !== "string" || key.length === 0) {
    throw new Error("feature flag key must be a non-empty string")
  }
  const { label, description, metadata, ...http } = opts
  const body: Record<string, unknown> = { enabled }
  if (label !== undefined) body.label = label
  if (description !== undefined) body.description = description
  if (metadata !== undefined) body.metadata = metadata
  const raw = await request<unknown>(
    "PUT",
    `/config/flags/${encodeURIComponent(key)}`,
    body,
    http,
  )
  return FeatureFlagSchema.parse(camelizeFlag(raw))
}

export async function isEnabled(
  key: string,
  opts: HttpOptions = {},
): Promise<boolean> {
  const flags = await list(opts)
  return flags.flags.find((flag) => flag.key === key)?.enabled ?? false
}

function camelizeFlag(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    key: src.key,
    label: src.label,
    description: src.description,
    enabled: src.enabled,
    source: src.source,
    metadata: src.metadata ?? {},
    updatedAt: src.updated_at ?? src.updatedAt ?? null,
  }
}

function camelizeList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    flags: Array.isArray(src.flags) ? src.flags.map(camelizeFlag) : src.flags,
  }
}

export const flags = {
  list,
  set,
  isEnabled,
}
