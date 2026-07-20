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

// Default per-request timeout (ms) that explicit `BackAI` clients apply. The
// env-configured singleton keeps its historical behaviour (no timeout unless
// the caller passes one), so the existing call sites are unaffected.
export const DEFAULT_TIMEOUT_MS = 30_000
// Explicit `BackAI` clients retry transient failures by default; the
// singleton does not (its effective `maxRetries` is 0). Mirrors
// `af_stack._http.DEFAULT_MAX_RETRIES`.
export const DEFAULT_MAX_RETRIES = 2

// Status codes worth retrying: rate limiting + transient upstream failures.
const RETRYABLE_STATUS = new Set([429, 500, 502, 503, 504])
// Only inherently-idempotent methods retry automatically; mutations retry
// solely when the caller supplies an `idempotencyKey` (which also sends the
// `Idempotency-Key` header so the server can dedupe). Mirrors the Python rule.
const IDEMPOTENT_METHODS = new Set(["GET", "HEAD", "OPTIONS"])
const RETRY_BASE_DELAY_MS = 500
const RETRY_MAX_DELAY_MS = 20_000

// ---------------------------------------------------------------------------
// Runtime-version compatibility policy (mirrors `af_stack._http`).
//
// The SDK lazily fetches `GET /api/v1/version` once per explicit client and
// *warns* (never fails) when the runtime's major version is out of range.
// 404 (older runtimes without the endpoint) is tolerated.
// ---------------------------------------------------------------------------
export const SUPPORTED_RUNTIME = ">=0.0.0,<1.0.0"
export const SUPPORTED_RUNTIME_MAJOR = 0

const SEMVER_RE = /^\s*v?(\d+)\.(\d+)\.(\d+)/

/**
 * Return a warning message when `version` is major-incompatible, else `null`.
 * Pure + side-effect free so it can be unit-tested without a network. Unknown
 * / unparseable / missing versions are tolerated (return `null`).
 */
export function checkRuntimeCompat(version: string | null | undefined): string | null {
  if (version === null || version === undefined || version === "") return null
  const m = SEMVER_RE.exec(String(version))
  if (m === null) return null
  const major = Number(m[1])
  if (major !== SUPPORTED_RUNTIME_MAJOR) {
    return (
      `BackAI runtime version ${version} (major ${major}) is outside the ` +
      `range this SDK supports (${SUPPORTED_RUNTIME}). Behaviour may be ` +
      `incompatible — upgrade the SDK.`
    )
  }
  return null
}

// ---------------------------------------------------------------------------
// Ambient client config (the TypeScript mirror of `af_stack`'s contextvar
// transport). An explicit `BackAI` client binds its `{baseUrl, apiKey,
// timeout, maxRetries}` here for the *synchronous* duration of each delegated
// call; the resolvers below consult it when a per-call override is absent,
// then fall back to `process.env`, then the built-in default. When unset
// (the singleton path) behaviour is identical to earlier releases.
// ---------------------------------------------------------------------------
export interface ClientConfig {
  baseUrl?: string
  apiKey?: string
  /** Per-request timeout in milliseconds. */
  timeout?: number
  /** Automatic retries for transient (429/5xx) failures. */
  maxRetries?: number
}

const ambient: { current: ClientConfig | null } = { current: null }

/** The active explicit-client config, or `null` on the singleton path. */
export function getAmbientConfig(): ClientConfig | null {
  return ambient.current
}

/**
 * Run `fn` with `config` bound as the ambient client config, restoring the
 * previous binding afterwards. Concurrency-safe: SDK functions read the
 * ambient config synchronously (before their first `await`), and there is no
 * `await` between binding and restoring here, so interleaved calls never
 * observe another call's config.
 */
export function runWithConfig<T>(config: ClientConfig, fn: () => T): T {
  const prev = ambient.current
  ambient.current = config
  try {
    return fn()
  } finally {
    ambient.current = prev
  }
}

/**
 * Async-generator variant of {@link runWithConfig}. The binding is held for
 * the lifetime of the returned iterable so a streaming method (whose body
 * only runs on the first `.next()`) still resolves config against the client.
 */
export function runGenWithConfig<T>(
  config: ClientConfig,
  gen: () => AsyncIterable<T>,
): AsyncIterable<T> {
  return (async function* () {
    const prev = ambient.current
    ambient.current = config
    try {
      yield* gen()
    } finally {
      ambient.current = prev
    }
  })()
}

export interface HttpOptions {
  /** Override `AF_STACK_URL` / the ambient client base URL. */
  baseUrl?: string
  /** Override `AF_STACK_API_KEY` / the ambient client API key. */
  apiKey?: string
  /** Extra headers merged into the request. */
  headers?: Record<string, string>
  /** AbortSignal for cancellation. */
  signal?: AbortSignal
  /** Request id; falls back to `ctx.requestId` or a generated value. */
  requestId?: string
  /** W3C `traceparent` for distributed tracing. */
  traceparent?: string
  /** Per-request timeout in milliseconds (aborts the fetch). */
  timeout?: number
  /** Max transient-failure retries for THIS call (overrides the client default). */
  maxRetries?: number
  /**
   * Idempotency-Key header value. Supplying it both dedupes the request
   * server-side AND makes an otherwise non-idempotent mutation eligible for
   * automatic retry (safe because the server dedupes replays).
   */
  idempotencyKey?: string
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

export function resolveBaseUrl(override?: string): string {
  if (override !== undefined && override !== "") return override.replace(/\/+$/, "")
  const amb = ambient.current?.baseUrl
  if (amb !== undefined && amb !== "") return amb.replace(/\/+$/, "")
  const fromEnv = envVar("AF_STACK_URL")
  if (fromEnv !== undefined && fromEnv !== "") return fromEnv.replace(/\/+$/, "")
  return DEFAULT_BASE_URL
}

export function resolveApiKey(override?: string): string | undefined {
  if (override !== undefined && override !== "") return override
  const amb = ambient.current?.apiKey
  if (amb !== undefined && amb !== "") return amb
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

function buildHeaders(
  opts: HttpOptions,
  hasBody: boolean,
): {
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

  if (opts.idempotencyKey !== undefined && opts.idempotencyKey !== "") {
    headers["idempotency-key"] = opts.idempotencyKey
  }

  if (opts.headers !== undefined) {
    for (const [k, v] of Object.entries(opts.headers)) headers[k.toLowerCase()] = v
  }

  return { headers, requestId }
}

/** Promise that resolves after `ms` milliseconds. */
function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/**
 * Compute the delay (ms) before the next retry attempt, honouring a
 * `Retry-After` header (numeric seconds or an HTTP-date, capped at 20s) and
 * otherwise using exponential backoff with full jitter. Exported for tests.
 */
export function retryDelayMs(response: Response, attempt: number): number {
  const retryAfter = response.headers.get("retry-after")
  if (retryAfter !== null && retryAfter !== "") {
    const asNum = Number(retryAfter)
    if (!Number.isNaN(asNum)) {
      return Math.max(0, Math.min(asNum * 1000, RETRY_MAX_DELAY_MS))
    }
    const when = Date.parse(retryAfter)
    if (!Number.isNaN(when)) {
      const delta = when - Date.now()
      if (delta > 0) return Math.min(delta, RETRY_MAX_DELAY_MS)
    }
  }
  const ceiling = Math.min(RETRY_BASE_DELAY_MS * 2 ** attempt, RETRY_MAX_DELAY_MS)
  return Math.random() * ceiling
}

/**
 * Combine an optional caller signal with an optional timeout into a single
 * AbortSignal. Returns the caller signal untouched when no timeout is set, so
 * the singleton path (no timeout) behaves exactly as before.
 */
function buildSignal(
  userSignal: AbortSignal | undefined,
  timeoutMs: number | undefined,
): AbortSignal | undefined {
  if (timeoutMs === undefined || timeoutMs <= 0) return userSignal
  const timeoutSignal = AbortSignal.timeout(timeoutMs)
  if (userSignal === undefined) return timeoutSignal
  const anyFn = (AbortSignal as { any?: (s: AbortSignal[]) => AbortSignal }).any
  if (anyFn !== undefined) return anyFn([userSignal, timeoutSignal])
  return userSignal
}

/**
 * Effective retry budget + timeout for a call: an explicit per-call option
 * wins, else the ambient client default, else 0 / no-timeout (singleton).
 */
function retryConfig(opts: HttpOptions): { maxRetries: number; timeout: number | undefined } {
  const amb = getAmbientConfig()
  return {
    maxRetries: Math.max(0, opts.maxRetries ?? amb?.maxRetries ?? 0),
    timeout: opts.timeout ?? amb?.timeout,
  }
}

async function parseError(response: Response, requestId: string): Promise<SuiteError> {
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

  const methodU = method.toUpperCase()
  const { maxRetries, timeout } = retryConfig(opts)
  const retryable = IDEMPOTENT_METHODS.has(methodU) || opts.idempotencyKey !== undefined
  const url = buildUrl(baseUrl, path)

  let attempt = 0
  // eslint-disable-next-line no-constant-condition
  while (true) {
    const init: RequestInit = { method, headers }
    if (hasBody) init.body = JSON.stringify(body)
    const signal = buildSignal(opts.signal, timeout)
    if (signal !== undefined) init.signal = signal

    const response = await fetchFn(url, init)
    if (response.ok) return response
    if (attempt < maxRetries && retryable && RETRYABLE_STATUS.has(response.status)) {
      await sleep(retryDelayMs(response, attempt))
      attempt += 1
      continue
    }
    throw await parseError(response, requestId)
  }
}

/** Raw body helper for multipart/binary endpoints. */
export async function rawBodyRequest(
  method: string,
  path: string,
  body: BodyInit | null,
  opts: HttpOptions = {},
  contentType?: string,
): Promise<Response> {
  const baseUrl = resolveBaseUrl(opts.baseUrl)
  const { headers, requestId } = buildHeaders(opts, false)
  if (contentType !== undefined && contentType !== "") {
    headers["content-type"] = contentType
  }

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

  const methodU = method.toUpperCase()
  const { maxRetries, timeout } = retryConfig(opts)
  const retryable = IDEMPOTENT_METHODS.has(methodU) || opts.idempotencyKey !== undefined
  const url = buildUrl(baseUrl, path)

  let attempt = 0
  // eslint-disable-next-line no-constant-condition
  while (true) {
    const init: RequestInit = { method, headers }
    if (body !== null) init.body = body
    const signal = buildSignal(opts.signal, timeout)
    if (signal !== undefined) init.signal = signal

    const response = await fetchFn(url, init)
    if (response.ok) return response
    if (attempt < maxRetries && retryable && RETRYABLE_STATUS.has(response.status)) {
      await sleep(retryDelayMs(response, attempt))
      attempt += 1
      continue
    }
    throw await parseError(response, requestId)
  }
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
  return (
    typeof v === "object" &&
    v !== null &&
    !Array.isArray(v) &&
    (Object.getPrototypeOf(v) === Object.prototype || Object.getPrototypeOf(v) === null)
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
export async function* parseSse(stream: ReadableStream<Uint8Array>): AsyncIterable<SseEvent> {
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
