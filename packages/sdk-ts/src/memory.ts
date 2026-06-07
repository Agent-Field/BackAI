// SPDX-License-Identifier: Apache-2.0

// suite.memory.* — scoped key/value + semantic memory primitives.
//
// Endpoint paths and JSON shapes are the canonical contract from
// `apps/dashboard/src/lib/api.ts` (the Phase 8 Memory section). Schemas
// below mirror the zod schemas there; if the dashboard contract moves,
// move these together.
//
// Public API stays camelCase; the wire stays snake_case. We translate at
// the boundary with the `_http.ts` helpers — except for `value` and
// `metadata`, which are caller-defined opaque payloads forwarded
// verbatim (the same pattern as `args` in jobs.ts).

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

// ---------- shared schemas (mirror lib/api.ts, camelCase) ----------

export const MemoryScopeSchema = z.enum([
  "global",
  "tenant",
  "agent",
  "session",
  "run",
])
export type MemoryScope = z.infer<typeof MemoryScopeSchema>

export const MemoryEntrySchema = z.object({
  scope: MemoryScopeSchema,
  scopeId: z.string().nullable(),
  key: z.string(),
  value: z.unknown(),
  metadata: z.record(z.string(), z.unknown()),
  hasEmbedding: z.boolean(),
  createdAt: z.string(),
  updatedAt: z.string(),
})
export type MemoryEntry = z.infer<typeof MemoryEntrySchema>

export const MemoryListSchema = z.object({
  entries: z.array(MemoryEntrySchema),
  total: z.number(),
  hasMore: z.boolean(),
})
export type MemoryList = z.infer<typeof MemoryListSchema>

export const MemorySearchHitSchema = z.object({
  entry: MemoryEntrySchema,
  similarity: z.number(),
  distance: z.number(),
})
export type MemorySearchHit = z.infer<typeof MemorySearchHitSchema>

export const MemorySearchResultSchema = z.object({
  hits: z.array(MemorySearchHitSchema),
  durationMs: z.number(),
})
export type MemorySearchResult = z.infer<typeof MemorySearchResultSchema>

// ---------- public API ----------

const ALLOWED_SCOPES: readonly MemoryScope[] = [
  "global",
  "tenant",
  "agent",
  "session",
  "run",
]

export interface PutMemoryOptions extends HttpOptions {
  scopeId?: string
  metadata?: Record<string, unknown>
  /** Ask the runtime to compute an embedding so the entry is searchable. */
  embed?: boolean
}

export interface ListMemoryOptions extends HttpOptions {
  scope?: MemoryScope | string
  scopeId?: string
  /** Server-side key-prefix match. */
  prefix?: string
  limit?: number
  offset?: number
}

export interface SearchMemoryOptions extends HttpOptions {
  scope?: MemoryScope | string
  scopeId?: string
  /** Top K hits to return (1–50, default 10). */
  topK?: number
  /** Minimum similarity threshold (0–1). */
  threshold?: number
}

/** Fetch a single memory entry by (scope, scopeId, key). */
export async function get(
  scope: MemoryScope | string,
  key: string,
  scopeId?: string,
  opts: HttpOptions = {},
): Promise<MemoryEntry> {
  validateScope(scope)
  if (typeof key !== "string" || key.length === 0) {
    throw new Error("memory key must be a non-empty string")
  }
  const qs = new URLSearchParams({ scope, key })
  if (scopeId !== undefined) qs.set("scope_id", scopeId)
  const raw = await request<unknown>(
    "GET",
    `/memory/get?${qs.toString()}`,
    null,
    opts,
  )
  return MemoryEntrySchema.parse(camelizeEntry(raw))
}

/** Upsert a memory entry and return the persisted row. */
export async function put(
  scope: MemoryScope | string,
  key: string,
  value: unknown,
  opts: PutMemoryOptions = {},
): Promise<MemoryEntry> {
  validateScope(scope)
  if (typeof key !== "string" || key.length === 0) {
    throw new Error("memory key must be a non-empty string")
  }
  const { scopeId, metadata, embed = false, ...http } = opts
  const body: Record<string, unknown> = {
    scope,
    key,
    value,
    embed: Boolean(embed),
  }
  if (scopeId !== undefined) body.scope_id = scopeId
  if (metadata !== undefined) body.metadata = metadata
  const raw = await request<unknown>("PUT", "/memory", body, http)
  return MemoryEntrySchema.parse(camelizeEntry(raw))
}

/** Delete a memory entry. Returns true on success. */
async function deleteEntry(
  scope: MemoryScope | string,
  key: string,
  scopeId?: string,
  opts: HttpOptions = {},
): Promise<boolean> {
  validateScope(scope)
  if (typeof key !== "string" || key.length === 0) {
    throw new Error("memory key must be a non-empty string")
  }
  const qs = new URLSearchParams({ scope, key })
  if (scopeId !== undefined) qs.set("scope_id", scopeId)
  const raw = await request<{ deleted?: boolean } | null>(
    "DELETE",
    `/memory?${qs.toString()}`,
    null,
    opts,
  )
  if (raw === null || raw === undefined) return true
  return Boolean(raw.deleted ?? true)
}
// Exported under both `delete` (matches lib/api.ts) and `deleteEntry`
// (because `delete` is a reserved word at some import positions).
export { deleteEntry as delete }

/** List memory entries with optional filters. */
export async function list(opts: ListMemoryOptions = {}): Promise<MemoryList> {
  const { scope, scopeId, prefix, limit = 100, offset = 0, ...http } = opts
  if (limit <= 0) throw new Error("limit must be a positive integer")
  if (offset < 0) throw new Error("offset must be non-negative")
  if (scope !== undefined) validateScope(scope)
  const qs = new URLSearchParams()
  if (scope !== undefined) qs.set("scope", scope)
  if (scopeId !== undefined) qs.set("scope_id", scopeId)
  if (prefix !== undefined && prefix !== "") qs.set("prefix", prefix)
  qs.set("limit", String(limit))
  qs.set("offset", String(offset))
  const raw = await request<unknown>(
    "GET",
    `/memory?${qs.toString()}`,
    null,
    http,
  )
  return MemoryListSchema.parse(camelizeList(raw))
}

/** Semantic search across embedded memory entries. */
export async function search(
  query: string,
  opts: SearchMemoryOptions = {},
): Promise<MemorySearchResult> {
  if (typeof query !== "string" || query.length === 0) {
    throw new Error("memory search query must be a non-empty string")
  }
  const { scope, scopeId, topK = 10, threshold, ...http } = opts
  if (topK < 1 || topK > 50) {
    throw new Error("topK must be between 1 and 50")
  }
  if (threshold !== undefined && (threshold < 0 || threshold > 1)) {
    throw new Error("threshold must be between 0 and 1")
  }
  if (scope !== undefined) validateScope(scope)
  const body: Record<string, unknown> = { query, top_k: topK }
  if (scope !== undefined) body.scope = scope
  if (scopeId !== undefined) body.scope_id = scopeId
  if (threshold !== undefined) body.threshold = threshold
  const raw = await request<unknown>("POST", "/memory/search", body, http)
  return MemorySearchResultSchema.parse(camelizeSearchResult(raw))
}

// ---------- helpers ----------

function validateScope(scope: string): void {
  if (!(ALLOWED_SCOPES as readonly string[]).includes(scope)) {
    throw new Error(
      `scope must be one of: ${ALLOWED_SCOPES.join(", ")}; got ${JSON.stringify(scope)}`,
    )
  }
}

/**
 * Translate the snake_case wire envelope into the camelCase shape declared
 * by `MemoryEntrySchema`, *without* touching `value` and `metadata` (both
 * are caller-defined opaque payloads forwarded verbatim — same pattern
 * as `args` in jobs.ts).
 */
function camelizeEntry(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    scope: src.scope,
    scopeId: src.scope_id ?? src.scopeId ?? null,
    key: src.key,
    value: src.value,
    metadata: src.metadata ?? {},
    hasEmbedding: src.has_embedding ?? src.hasEmbedding ?? false,
    createdAt: src.created_at ?? src.createdAt,
    updatedAt: src.updated_at ?? src.updatedAt,
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

function camelizeHit(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    entry: camelizeEntry(src.entry),
    similarity: src.similarity,
    distance: src.distance,
  }
}

function camelizeSearchResult(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const hits = Array.isArray(src.hits) ? src.hits.map(camelizeHit) : src.hits
  return {
    hits,
    durationMs: src.duration_ms ?? src.durationMs ?? 0,
  }
}

/** Namespace object — the shape `suite.memory` is built from. */
export const memory = {
  get,
  put,
  delete: deleteEntry,
  list,
  search,
} as const