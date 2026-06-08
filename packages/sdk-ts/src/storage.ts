// SPDX-License-Identifier: Apache-2.0

// suite.storage.* — object storage operations.
//
// Endpoint paths and JSON shapes are the canonical contract from
// `apps/dashboard/src/lib/api.ts` (the Phase 5 Storage section).
//
// Upload uses multipart/form-data because the dashboard's `api.storage.upload`
// already speaks that wire format. We construct FormData manually so the
// helper works in Node, Bun, Deno, and edge runtimes.

import { z } from "zod"
import { rawRequest, request, SuiteError, type HttpOptions } from "./_http.js"

const DEFAULT_BASE_URL = "http://localhost:8080"
const PATH_PREFIX = "/api/v1"

// ---------- shared schemas (mirror lib/api.ts) ----------

export const StorageObjectSchema = z.object({
  key: z.string(),
  size: z.number(),
  contentType: z.string().nullable(),
  lastModified: z.string(),
  etag: z.string().nullable(),
})
export type StorageObject = z.infer<typeof StorageObjectSchema>

export const StorageListSchema = z.object({
  objects: z.array(StorageObjectSchema),
  prefix: z.string(),
  nextToken: z.string().nullable(),
})
export type StorageList = z.infer<typeof StorageListSchema>

export const SignedURLSchema = z.object({
  key: z.string(),
  url: z.string(),
  expiresAt: z.string(),
})
export type SignedURL = z.infer<typeof SignedURLSchema>

// ---------- public API ----------

export interface UploadOptions extends HttpOptions {
  /** MIME type — defaults to `application/octet-stream`. */
  contentType?: string
}

export interface ListStorageOptions extends HttpOptions {
  prefix?: string
  limit?: number
}

export interface DownloadStorageOptions extends HttpOptions {
  /** Resize target width in pixels. */
  width?: number
  /** Resize target height in pixels. */
  height?: number
  /** `thumbnail` preserves aspect ratio within the target box; `resize` uses exact dimensions when both are set. */
  transform?: "resize" | "thumbnail"
  /** Output format for transformed images. */
  format?: "png" | "jpeg" | "jpg" | "gif"
}

/** Upload bytes to `key`. Returns the persisted object metadata. */
export async function upload(
  data: Uint8Array | ArrayBuffer | Blob | string,
  key: string,
  opts: UploadOptions = {},
): Promise<StorageObject> {
  if (typeof key !== "string" || key.length === 0) {
    throw new Error("storage key must be a non-empty string")
  }
  const { contentType, ...http } = opts
  const blob = toBlob(data, contentType ?? "application/octet-stream")
  const form = new FormData()
  form.set("key", key)
  form.set("file", blob, key)

  const baseUrl = resolveBaseUrl(http.baseUrl)
  const url = `${baseUrl}${PATH_PREFIX}/storage/upload`
  const headers = buildUploadHeaders(http)
  const requestId = headers["x-request-id"]
  const fetchFn = (globalThis as { fetch?: typeof fetch }).fetch
  if (fetchFn === undefined) {
    throw new SuiteError({
      code: "NO_FETCH",
      message: "globalThis.fetch is not available in this runtime",
      status: 0,
      requestId,
      details: null,
    })
  }

  const init: RequestInit = {
    method: "POST",
    headers,
    body: form,
  }
  if (http.signal !== undefined) init.signal = http.signal

  const response = await fetchFn(url, init)
  if (!response.ok) throw await parseError(response, requestId)
  const raw = await response.json()
  return StorageObjectSchema.parse(camelizeStorageObject(raw))
}

/** Download an object's bytes by key. */
export async function download(
  key: string,
  opts: DownloadStorageOptions = {},
): Promise<Uint8Array> {
  if (typeof key !== "string" || key.length === 0) {
    throw new Error("storage key must be a non-empty string")
  }
  const { width, height, transform, format, ...http } = opts
  const qs = new URLSearchParams()
  if (width !== undefined) {
    if (width <= 0) throw new Error("width must be a positive integer")
    qs.set("width", String(width))
  }
  if (height !== undefined) {
    if (height <= 0) throw new Error("height must be a positive integer")
    qs.set("height", String(height))
  }
  if (transform !== undefined) qs.set("transform", transform)
  if (format !== undefined) qs.set("format", format)
  const headers = { ...(opts.headers ?? {}), accept: "application/octet-stream" }
  const path = `/storage/${encodeURIComponent(key)}${qs.size > 0 ? `?${qs.toString()}` : ""}`
  const response = await rawRequest(
    "GET",
    path,
    null,
    { ...http, headers },
  )
  const buf = await response.arrayBuffer()
  return new Uint8Array(buf)
}

/** Return a short-lived pre-signed URL for `key`. */
export async function signedURL(
  key: string,
  ttlSeconds = 3600,
  opts: HttpOptions = {},
): Promise<SignedURL> {
  if (typeof key !== "string" || key.length === 0) {
    throw new Error("storage key must be a non-empty string")
  }
  if (ttlSeconds <= 0) {
    throw new Error("ttlSeconds must be a positive integer")
  }
  const qs = new URLSearchParams({ key, ttl: String(ttlSeconds) })
  const raw = await request<unknown>(
    "GET",
    `/storage/signed-url?${qs.toString()}`,
    null,
    opts,
  )
  return SignedURLSchema.parse(camelizeSignedURL(raw))
}

/** Alias for parity with snake_case docs / Python SDK. */
export const signed_url = signedURL

/** Delete the object at `key`. Returns true on success. */
async function deleteObject(
  key: string,
  opts: HttpOptions = {},
): Promise<boolean> {
  if (typeof key !== "string" || key.length === 0) {
    throw new Error("storage key must be a non-empty string")
  }
  const raw = await request<{ deleted?: boolean } | null>(
    "DELETE",
    `/storage/${encodeURIComponent(key)}`,
    null,
    opts,
  )
  if (raw === null || raw === undefined) return true
  return Boolean(raw.deleted ?? true)
}
// Exported under both `delete` (matches lib/api.ts) and `deleteObject`
// (because `delete` is a reserved word in some import positions).
export { deleteObject as delete }

/** List objects, optionally restricted to `prefix`. */
export async function list(
  opts: ListStorageOptions = {},
): Promise<StorageList> {
  const { prefix, limit = 100, ...http } = opts
  const qs = new URLSearchParams()
  if (prefix !== undefined && prefix !== "") qs.set("prefix", prefix)
  qs.set("limit", String(limit))
  const raw = await request<unknown>(
    "GET",
    `/storage?${qs.toString()}`,
    null,
    http,
  )
  return StorageListSchema.parse(camelizeStorageList(raw))
}

// ---------- helpers ----------

function toBlob(
  data: Uint8Array | ArrayBuffer | Blob | string,
  contentType: string,
): Blob {
  if (data instanceof Blob) return data
  if (typeof data === "string") return new Blob([data], { type: contentType })
  if (data instanceof ArrayBuffer) return new Blob([data], { type: contentType })
  // Uint8Array → wrap as ArrayBuffer-backed BlobPart. The generic ArrayBufferLike
  // (which can be a SharedArrayBuffer) isn't assignable to BlobPart in strict TS,
  // so we copy through a fresh ArrayBuffer to widen the type safely.
  const copy = new Uint8Array(data.byteLength)
  copy.set(data)
  return new Blob([copy.buffer], { type: contentType })
}

function envVar(name: string): string | undefined {
  const g = globalThis as { process?: { env?: Record<string, string | undefined> } }
  return g.process?.env?.[name]
}

function resolveBaseUrl(override?: string): string {
  if (override !== undefined && override !== "") return override.replace(/\/+$/, "")
  const fromEnv = envVar("AF_STACK_URL")
  if (fromEnv !== undefined && fromEnv !== "") return fromEnv.replace(/\/+$/, "")
  return DEFAULT_BASE_URL
}

function resolveApiKey(override?: string): string | undefined {
  if (override !== undefined && override !== "") return override
  const fromEnv = envVar("AF_STACK_API_KEY")
  if (fromEnv !== undefined && fromEnv !== "") return fromEnv
  return undefined
}

function generateRequestId(): string {
  const g = globalThis as { crypto?: { randomUUID?: () => string } }
  if (g.crypto?.randomUUID !== undefined) {
    return `req_${g.crypto.randomUUID().replace(/-/g, "")}`
  }
  return `req_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`
}

function buildUploadHeaders(opts: HttpOptions): Record<string, string> {
  // We deliberately do NOT set content-type — letting the runtime pick the
  // multipart boundary is what FormData expects.
  const headers: Record<string, string> = { accept: "application/json" }
  const apiKey = resolveApiKey(opts.apiKey)
  if (apiKey !== undefined) headers.authorization = `Bearer ${apiKey}`
  const requestId = opts.requestId ?? generateRequestId()
  headers["x-request-id"] = requestId
  if (opts.traceparent !== undefined) headers.traceparent = opts.traceparent
  if (opts.headers !== undefined) {
    for (const [k, v] of Object.entries(opts.headers)) headers[k.toLowerCase()] = v
  }
  return headers
}

async function parseError(response: Response, requestId: string): Promise<SuiteError> {
  let body: { error?: { code?: string; message?: string; request_id?: string; details?: unknown } } = {}
  try {
    body = (await response.json()) as typeof body
  } catch {
    // not JSON
  }
  const err = body.error ?? {}
  return new SuiteError({
    code: err.code ?? `HTTP_${response.status}`,
    message: err.message ?? `Request failed with status ${response.status}`,
    status: response.status,
    requestId: err.request_id ?? requestId,
    details: err.details ?? null,
  })
}

function camelKey(key: string): string {
  return key.replace(/_([a-z0-9])/g, (_, c: string) => c.toUpperCase())
}

function camelizeStorageObject(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(src)) out[camelKey(k)] = v
  return out
}

function camelizeStorageList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const objects = Array.isArray(src.objects)
    ? src.objects.map(camelizeStorageObject)
    : src.objects
  return {
    objects,
    prefix: src.prefix,
    nextToken: src.next_token ?? src.nextToken ?? null,
  }
}

function camelizeSignedURL(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    key: src.key,
    url: src.url,
    expiresAt: src.expires_at ?? src.expiresAt,
  }
}

/** Namespace object — the shape `suite.storage` is built from. */
export const storage = {
  upload,
  download,
  signedURL,
  delete: deleteObject,
  list,
} as const
