// SPDX-License-Identifier: Apache-2.0

// suite.search.* — tenant-scoped app-data search.
//
// This indexes product/workload data, not AgentField memory/runs/traces.
// Public API is camelCase; wire JSON remains snake_case.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

export const SearchModeSchema = z.enum(["fts", "vector", "hybrid"])
export type SearchMode = z.infer<typeof SearchModeSchema>

export const SearchDocumentSchema = z.object({
  tenantId: z.string(),
  namespace: z.string(),
  key: z.string(),
  title: z.string(),
  body: z.string(),
  metadata: z.record(z.string(), z.unknown()),
  hasEmbedding: z.boolean(),
  createdAt: z.string(),
  updatedAt: z.string(),
})
export type SearchDocument = z.infer<typeof SearchDocumentSchema>

export const SearchHitSchema = z.object({
  document: SearchDocumentSchema,
  score: z.number(),
  ftsRank: z.number().optional(),
  similarity: z.number().optional(),
})
export type SearchHit = z.infer<typeof SearchHitSchema>

export const SearchResultSchema = z.object({
  hits: z.array(SearchHitSchema),
  mode: SearchModeSchema,
  durationMs: z.number(),
})
export type SearchResult = z.infer<typeof SearchResultSchema>

export interface SearchOptions extends HttpOptions {
  mode?: SearchMode
  namespace?: string
  metadataFilter?: Record<string, unknown>
  limit?: number
  offset?: number
}

export interface UpsertSearchDocumentOptions extends HttpOptions {
  namespace?: string
  title?: string
  body?: string
  metadata?: Record<string, unknown>
  embed?: boolean
}

export interface DeleteSearchDocumentOptions extends HttpOptions {
  namespace?: string
}

export async function search(
  query: string,
  modeOrOptions: SearchMode | SearchOptions = {},
): Promise<SearchResult> {
  if (typeof query !== "string" || query.trim() === "") {
    throw new Error("search query must be a non-empty string")
  }
  const opts: SearchOptions =
    typeof modeOrOptions === "string" ? { mode: modeOrOptions } : modeOrOptions
  const {
    mode = "hybrid",
    namespace,
    metadataFilter,
    limit = 10,
    offset = 0,
    ...http
  } = opts
  validateMode(mode)
  if (limit < 1 || limit > 100) throw new Error("limit must be between 1 and 100")
  if (offset < 0) throw new Error("offset must be non-negative")

  const body: Record<string, unknown> = {
    query,
    mode,
    limit,
    offset,
  }
  if (namespace !== undefined && namespace !== "") body.namespace = namespace
  if (metadataFilter !== undefined) body.metadata_filter = metadataFilter

  const raw = await request<unknown>("POST", "/search", body, http)
  return SearchResultSchema.parse(camelizeResult(raw))
}

export async function upsert(
  key: string,
  opts: UpsertSearchDocumentOptions = {},
): Promise<SearchDocument> {
  if (typeof key !== "string" || key.length === 0) {
    throw new Error("search document key must be a non-empty string")
  }
  const {
    namespace,
    title = "",
    body = "",
    metadata,
    embed = false,
    ...http
  } = opts
  if (title.trim() === "" && body.trim() === "") {
    throw new Error("search document title or body is required")
  }
  const payload: Record<string, unknown> = { key, title, body, embed }
  if (namespace !== undefined && namespace !== "") payload.namespace = namespace
  if (metadata !== undefined) payload.metadata = metadata

  const raw = await request<unknown>("PUT", "/search/documents", payload, http)
  return SearchDocumentSchema.parse(camelizeDocument(raw))
}

async function deleteDocument(
  key: string,
  opts: DeleteSearchDocumentOptions = {},
): Promise<boolean> {
  if (typeof key !== "string" || key.length === 0) {
    throw new Error("search document key must be a non-empty string")
  }
  const { namespace = "default", ...http } = opts
  const path = `/search/documents/${encodeURIComponent(namespace)}/${encodeURIComponent(key)}`
  const raw = await request<{ deleted?: boolean } | null>("DELETE", path, null, http)
  if (raw === null || raw === undefined) return true
  return Boolean(raw.deleted ?? true)
}
export { deleteDocument as delete }

function validateMode(mode: string): void {
  if (!["fts", "vector", "hybrid"].includes(mode)) {
    throw new Error(`search mode must be fts, vector, or hybrid; got ${JSON.stringify(mode)}`)
  }
}

function camelizeDocument(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    tenantId: src.tenant_id ?? src.tenantId,
    namespace: src.namespace,
    key: src.key,
    title: src.title,
    body: src.body,
    metadata: src.metadata ?? {},
    hasEmbedding: src.has_embedding ?? src.hasEmbedding ?? false,
    createdAt: src.created_at ?? src.createdAt,
    updatedAt: src.updated_at ?? src.updatedAt,
  }
}

function camelizeHit(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    document: camelizeDocument(src.document),
    score: src.score,
    ftsRank: src.fts_rank ?? src.ftsRank,
    similarity: src.similarity,
  }
}

function camelizeResult(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const hits = Array.isArray(src.hits) ? src.hits.map(camelizeHit) : src.hits
  return {
    hits,
    mode: src.mode,
    durationMs: src.duration_ms ?? src.durationMs,
  }
}

export const searchIndex = {
  search,
  upsert,
  delete: deleteDocument,
}
