// suite.llm.* — OpenAI-compatible LLM gateway operations.
//
// Every model call inside the suite flows through this gateway. The wire
// shape mirrors the OpenAI Chat Completions / Embeddings API so callers
// can use any OpenAI-compatible client (e.g. the `openai` npm package)
// by pointing it at `/api/v1/llm`. The functions below are ergonomic
// shortcuts around the same surface.
//
// Endpoint paths and JSON shapes are the canonical contract from
// `apps/dashboard/src/lib/api.ts` (Phase 7 section). The zod schemas
// below mirror the dashboard's `LLMModelSchema`, `LLMModelListSchema`,
// and `CacheStatsSchema`.

import { z } from "zod"
import {
  parseSse,
  rawRequest,
  request,
  toCamel,
  type HttpOptions,
} from "./_http.js"

// ---------- shared schemas (mirror lib/api.ts, camelCase form) ----------

export const LLMModelSchema = z.object({
  id: z.string(),
  displayName: z.string(),
  provider: z.string(),
  promptUsdPer1m: z.number(),
  completionUsdPer1m: z.number(),
  supportsStreaming: z.boolean(),
  supportsTools: z.boolean(),
})
export type LLMModel = z.infer<typeof LLMModelSchema>

export const LLMModelListSchema = z.object({
  models: z.array(LLMModelSchema),
})
export type LLMModelList = z.infer<typeof LLMModelListSchema>

export const CacheStatsSchema = z.object({
  totalCalls: z.number(),
  cacheHits: z.number(),
  cacheMisses: z.number(),
  hitRate: z.number(),
  savingsUsd: z.number(),
  entries: z.number(),
})
export type CacheStats = z.infer<typeof CacheStatsSchema>

// ---------- public types ----------

/**
 * Raw OpenAI-shaped chat message. The SDK doesn't try to constrain
 * `content` — providers may accept multi-modal arrays, tool results,
 * or plain strings, and we forward the payload verbatim.
 */
export interface ChatMessage {
  role: "system" | "user" | "assistant" | "tool" | "developer"
  content: unknown
  name?: string
  tool_call_id?: string
  tool_calls?: unknown
}

/**
 * One streaming chunk from `chat({stream: true})`. Matches the OpenAI
 * `chat.completion.chunk` object shape — we don't reshape it so callers
 * can hand it straight to existing OpenAI-compatible code.
 */
export interface ChatChunk {
  id?: string
  object?: string
  model?: string
  choices?: Array<{
    index?: number
    delta?: { role?: string; content?: string; tool_calls?: unknown }
    finish_reason?: string | null
  }>
  [k: string]: unknown
}

export interface ChatOptions extends HttpOptions {
  /** Sampling temperature (provider-specific bounds). */
  temperature?: number
  /** Cap on response tokens. */
  maxTokens?: number
  /** Top-p sampling parameter. */
  topP?: number
  /** Tool definitions (OpenAI shape). */
  tools?: unknown[]
  /** Tool-choice strategy. */
  toolChoice?: unknown
  /** Pass-through for any provider-specific extras. */
  extra?: Record<string, unknown>
}

export interface EmbedOptions extends HttpOptions {
  /** Encoding format ("float" | "base64"). */
  encodingFormat?: "float" | "base64"
  /** Output dimension cap for models that support it. */
  dimensions?: number
  /** Pass-through for any provider-specific extras. */
  extra?: Record<string, unknown>
}

// ---------- chat ----------

/**
 * Non-streaming chat completion. Returns the raw OpenAI-shaped response
 * `Response` object so callers can introspect headers (cost, cache-hit,
 * tenant id) before parsing the body.
 *
 * Most callers want `.json()`:
 *   const res = await chat({ model, messages })
 *   const body = await res.json()
 */
export async function chat(input: {
  model: string
  messages: ChatMessage[]
  stream?: false
} & ChatOptions): Promise<Response>
/**
 * Streaming chat completion. Returns an async iterable of `ChatChunk`
 * objects decoded from the gateway's SSE stream. The iterator stops at
 * the OpenAI `[DONE]` sentinel.
 */
export async function chat(input: {
  model: string
  messages: ChatMessage[]
  stream: true
} & ChatOptions): Promise<AsyncIterable<ChatChunk>>
export async function chat(
  input: {
    model: string
    messages: ChatMessage[]
    stream?: boolean
  } & ChatOptions,
): Promise<Response | AsyncIterable<ChatChunk>> {
  const {
    model,
    messages,
    stream = false,
    temperature,
    maxTokens,
    topP,
    tools,
    toolChoice,
    extra,
    ...http
  } = input
  if (typeof model !== "string" || model.length === 0) {
    throw new Error("model must be a non-empty string")
  }
  if (!Array.isArray(messages) || messages.length === 0) {
    throw new Error("messages must be a non-empty array")
  }

  const body: Record<string, unknown> = { model, messages }
  if (temperature !== undefined) body.temperature = temperature
  if (maxTokens !== undefined) body.max_tokens = maxTokens
  if (topP !== undefined) body.top_p = topP
  if (tools !== undefined) body.tools = tools
  if (toolChoice !== undefined) body.tool_choice = toolChoice
  if (extra !== undefined) {
    for (const [k, v] of Object.entries(extra)) body[k] = v
  }
  if (stream) body.stream = true

  if (!stream) {
    return rawRequest("POST", "/llm/chat/completions", body, http)
  }

  const sseHeaders = { ...(http.headers ?? {}), accept: "text/event-stream" }
  const response = await rawRequest(
    "POST",
    "/llm/chat/completions",
    body,
    { ...http, headers: sseHeaders },
  )
  if (response.body === null) {
    return emptyAsyncIterable()
  }
  return streamChunks(response.body)
}

async function* emptyAsyncIterable(): AsyncIterable<ChatChunk> {
  // intentionally empty
}

async function* streamChunks(
  body: ReadableStream<Uint8Array>,
): AsyncIterable<ChatChunk> {
  for await (const event of parseSse(body)) {
    const data = event.data
    if (data === "[DONE]") return
    if (typeof data === "object" && data !== null) {
      yield data as ChatChunk
    }
  }
}

// ---------- embed ----------

/**
 * Compute embeddings via the OpenAI-compatible endpoint. Returns the
 * raw `Response`; call `.json()` to read `{model, data: [{embedding}], usage}`.
 */
export async function embed(
  input: {
    model: string
    input: string | string[]
  } & EmbedOptions,
): Promise<Response> {
  const { model, input: text, encodingFormat, dimensions, extra, ...http } = input
  if (typeof model !== "string" || model.length === 0) {
    throw new Error("model must be a non-empty string")
  }
  if (text === undefined || text === null) {
    throw new Error("input must be a non-empty string or array")
  }
  if (Array.isArray(text) && text.length === 0) {
    throw new Error("input must be a non-empty string or array")
  }
  const body: Record<string, unknown> = { model, input: text }
  if (encodingFormat !== undefined) body.encoding_format = encodingFormat
  if (dimensions !== undefined) body.dimensions = dimensions
  if (extra !== undefined) {
    for (const [k, v] of Object.entries(extra)) body[k] = v
  }
  return rawRequest("POST", "/llm/embeddings", body, http)
}

// ---------- models ----------

/** List all models exposed by the gateway. */
export async function models(opts: HttpOptions = {}): Promise<LLMModelList> {
  const raw = await request<unknown>("GET", "/llm/models", null, opts)
  return LLMModelListSchema.parse(toCamel(raw))
}

/** Fetch aggregated cache statistics for the gateway. */
export async function cacheStats(opts: HttpOptions = {}): Promise<CacheStats> {
  const raw = await request<unknown>("GET", "/llm/cache/stats", null, opts)
  return CacheStatsSchema.parse(toCamel(raw))
}

/** Namespace object — the shape `suite.llm` is built from. */
export const llm = {
  chat,
  embed,
  models,
  cacheStats,
} as const
