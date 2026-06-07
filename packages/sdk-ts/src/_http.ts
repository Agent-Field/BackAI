// SPDX-License-Identifier: Apache-2.0

// Internal HTTP client backed by global fetch.
//
// Works in Node 20+, Bun, Deno, Cloudflare Workers, Vercel Edge — anything
// with `globalThis.fetch`. No axios, no node-fetch.
//
// Reads base URL and API key from env (lazily, so tests can mutate
// process.env). Per-request overrides take precedence.
//
// JSON over the wire is snake_case; helpers in this file convert between
// snake_case and camelCase at the boundary so callers see camelCase only.

import { ctx } from "./ctx.js"

const DEFAULT_BASE_URL = "http://localhost:8080"
const PATH_PREFIX = "/api/v1"

export interface HttpOptions {
  /** Override `AF_STACK_URL`. */
  baseUrl?: string
  /** Override `AF_STACK_API_KEY`. */
  apiKey?: string
  /** Extra headers merged into the request. */
  headers?: Record<string, string>
  /** AbortSignal for cancellation. */
  signal?: AbortSignal
  /** Request id; falls back to `ctx.requestId` or a generated value. */
  requestId?: string
  /** W3C `traceparent` for distributed tracing. */
  traceparent?: string
}

export interface StructuredErrorBody {
  error?: {
    code?: string
    message?: string
    request_id?: string
    details?: unknown
  }
}

/**
 * Structured error mirroring TECH-SPEC.md §13. Thrown by every HTTP helper
 * on non-2xx responses so callers can branch on `.code` / `.status`.
 */
export class SuiteError extends Error {
  public readonly code: string
  public readonly status: number
  public readonly requestId: string | null
  public readonly details: unknown

  constructor(opts: {
    code: string
    message: string
    status: number
    requestId: string | null
    details: unknown
  }) {
    super(opts.message)
    this.name = "SuiteError"
    this.code = opts.code
    this.status = opts.status
    this.requestId = opts.requestId
    this.details = opts.details
  }
}

function envVar(name: string): string | undefined {
  // Use globalThis.process so this still type-checks in edge runtimes.
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
  // Cryptographically-strong when available; predictable fallback
  // otherwise. The format mirrors what the gateway accepts.
  const g = globalThis as { crypto?: { randomUUID?: () => string } }
  if (g.crypto?.randomUUID !== undefined) {
    return `req_${g.crypto.randomUUID().replace(/-/g, "")}`
  }
  return `req_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`
}

function buildUrl(baseUrl: string, path: string): string {
  const cleanPath = path.startsWith("/") ? path : `/${path}`
  return `${baseUrl}${PATH_PREFIX}${cleanPath}`
}

function buildHeaders(opts: HttpOptions, hasBody: boolean): {
  headers: Record<string, string>
  requestId: string
} {
  const headers: Record<string, string> = {
    accept: "application/json",
  }
  if (hasBody) headers["content-type"] = "application/json"

  const apiKey = resolveApiKey(opts.apiKey)
  if (apiKey !== undefined) headers.authorization = `Bearer ${apiKey}`

  const requestId = opts.requestId ?? ctx.requestId ?? generateRequestId()
  headers["x-request-id"] = requestId

  if (opts.traceparent !== undefined) headers.traceparent = opts.traceparent

  if (opts.headers !== undefined) {
    for (const [k, v] of Object.entries(opts.headers)) headers[k.toLowerCase()] = v
  }

  return { headers, requestId }
}

async function parseError(
  response: Response,
  requestId: string,
): Promise<SuiteError> {
  let body: StructuredErrorBody = {}
  try {
    body = (await response.json()) as StructuredErrorBody
  } catch {
    // Body wasn't JSON — fall through with empty body.
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

/** POST/GET/DELETE helper returning the raw fetch Response. */
export async function rawRequest(
  method: string,
  path: string,
  body: unknown,
  opts: HttpOptions = {},
): Promise<Response> {
  const baseUrl = resolveBaseUrl(opts.baseUrl)
  const hasBody = body !== undefined && body !== null
  const { headers, requestId } = buildHeaders(opts, hasBody)

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
    method,
    headers,
  }
  if (hasBody) init.body = JSON.stringify(body)
  if (opts.signal !== undefined) init.signal = opts.signal

  const url = buildUrl(baseUrl, path)
  const response = await fetchFn(url, init)
  if (!response.ok) throw await parseError(response, requestId)
  return response
}

/** Request + parse JSON. */
export async function request<T = unknown>(
  method: string,
  path: string,
  body: unknown,
  opts: HttpOptions = {},
): Promise<T> {
  const response = await rawRequest(method, path, body, opts)
  if (response.status === 204) return undefined as T
  return (await response.json()) as T
}

// ---------- camelCase / snake_case translation ----------

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v) && (
    Object.getPrototypeOf(v) === Object.prototype || Object.getPrototypeOf(v) === null
  )
}

function snakeKey(key: string): string {
  return key.replace(/([A-Z])/g, "_$1").toLowerCase()
}

function camelKey(key: string): string {
  return key.replace(/_([a-z0-9])/g, (_, c: string) => c.toUpperCase())
}

/** Deep-convert camelCase keys to snake_case before sending over the wire. */
export function toSnake<T = unknown>(value: unknown): T {
  if (Array.isArray(value)) return value.map((v) => toSnake(v)) as unknown as T
  if (isPlainObject(value)) {
    const out: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(value)) out[snakeKey(k)] = toSnake(v)
    return out as T
  }
  return value as T
}

/** Deep-convert snake_case keys to camelCase coming back from the wire. */
export function toCamel<T = unknown>(value: unknown): T {
  if (Array.isArray(value)) return value.map((v) => toCamel(v)) as unknown as T
  if (isPlainObject(value)) {
    const out: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(value)) out[camelKey(k)] = toCamel(v)
    return out as T
  }
  return value as T
}

// ---------- SSE parser used by agents.stream() ----------

export interface SseEvent {
  /** Event name from the `event:` field, or `null` for default. */
  event: string | null
  /** Decoded JSON payload from concatenated `data:` lines, or raw string. */
  data: unknown
  /** `id:` field if present. */
  id: string | null
}

/**
 * Parse a `text/event-stream` ReadableStream into an async iterable of
 * SSE events. Follows the WHATWG event-stream spec for line splitting.
 */
export async function* parseSse(
  stream: ReadableStream<Uint8Array>,
): AsyncIterable<SseEvent> {
  const reader = stream.getReader()
  const decoder = new TextDecoder("utf-8")
  let buffer = ""

  try {
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })

      let idx: number
      // Event records are separated by a blank line (\n\n or \r\n\r\n).
      while ((idx = buffer.search(/\r?\n\r?\n/)) !== -1) {
        const rawRecord = buffer.slice(0, idx)
        buffer = buffer.slice(idx).replace(/^\r?\n\r?\n/, "")
        const evt = parseSseRecord(rawRecord)
        if (evt !== null) yield evt
      }
    }
    if (buffer.trim().length > 0) {
      const evt = parseSseRecord(buffer)
      if (evt !== null) yield evt
    }
  } finally {
    reader.releaseLock()
  }
}

function parseSseRecord(record: string): SseEvent | null {
  let event: string | null = null
  let id: string | null = null
  const dataLines: string[] = []
  for (const rawLine of record.split(/\r?\n/)) {
    if (rawLine === "" || rawLine.startsWith(":")) continue
    const colonIdx = rawLine.indexOf(":")
    const field = colonIdx === -1 ? rawLine : rawLine.slice(0, colonIdx)
    const valueRaw = colonIdx === -1 ? "" : rawLine.slice(colonIdx + 1)
    const value = valueRaw.startsWith(" ") ? valueRaw.slice(1) : valueRaw
    if (field === "event") event = value
    else if (field === "id") id = value
    else if (field === "data") dataLines.push(value)
  }
  if (dataLines.length === 0 && event === null && id === null) return null
  const dataStr = dataLines.join("\n")
  let parsed: unknown = dataStr
  if (dataStr.length > 0) {
    try {
      parsed = JSON.parse(dataStr)
    } catch {
      parsed = dataStr
    }
  }
  return { event, data: parsed, id }
}