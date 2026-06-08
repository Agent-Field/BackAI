// SPDX-License-Identifier: Apache-2.0

// suite.llm.* — OpenAI-compatible LLM gateway operations.
//
// Every model call inside the suite flows through this gateway. The wire
// shape mirrors the OpenAI Chat Completions / Embeddings API so callers
// can use any OpenAI-compatible client (e.g. the `openai` npm package)
// by pointing it at `/api/v1/llm`. The functions below are ergonomic
// shortcuts around the same surface; embeddings use the top-level
// `/api/v1/embeddings` suite path by default.
//
// Endpoint paths and JSON shapes are the canonical contract from
// `apps/dashboard/src/lib/api.ts` (Phase 7 section). The zod schemas
// below mirror the dashboard's `LLMModelSchema`, `LLMModelListSchema`,
// and `CacheStatsSchema`.

import { z } from "zod"
import {
  parseSse,
  rawBodyRequest,
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

export interface ImageOptions extends HttpOptions {
  model?: string
  n?: number
  size?: string
  quality?: string
  style?: string
  responseFormat?: "url" | "b64_json"
  user?: string
  extra?: Record<string, unknown>
}

export interface SpeechOptions extends HttpOptions {
  model: string
  input: string
  voice: string
  responseFormat?: "mp3" | "opus" | "aac" | "flac" | "wav" | "pcm"
  speed?: number
  instructions?: string
  extra?: Record<string, unknown>
}

export interface TranscriptionOptions extends HttpOptions {
  model: string
  file: Blob
  filename?: string
  language?: string
  prompt?: string
  responseFormat?: "json" | "text" | "srt" | "verbose_json" | "vtt"
  temperature?: number
  extra?: Record<string, string | Blob>
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
  return rawRequest("POST", "/embeddings", body, http)
}

// ---------- images ----------

/** Generate images via the OpenAI-compatible image endpoint. */
export async function image(
  input: { prompt: string } & ImageOptions,
): Promise<Response> {
  const {
    prompt,
    model,
    n,
    size,
    quality,
    style,
    responseFormat,
    user,
    extra,
    ...http
  } = input
  if (typeof prompt !== "string" || prompt.length === 0) {
    throw new Error("prompt must be a non-empty string")
  }
  const body: Record<string, unknown> = { prompt }
  if (model !== undefined) body.model = model
  if (n !== undefined) body.n = n
  if (size !== undefined) body.size = size
  if (quality !== undefined) body.quality = quality
  if (style !== undefined) body.style = style
  if (responseFormat !== undefined) body.response_format = responseFormat
  if (user !== undefined) body.user = user
  if (extra !== undefined) {
    for (const [k, v] of Object.entries(extra)) body[k] = v
  }
  return rawRequest("POST", "/images/generations", body, http)
}

// ---------- audio ----------

/** Generate speech audio. Returns a raw Response so callers can read bytes. */
export async function speech(input: SpeechOptions): Promise<Response> {
  const {
    model,
    input: text,
    voice,
    responseFormat,
    speed,
    instructions,
    extra,
    ...http
  } = input
  if (typeof model !== "string" || model.length === 0) {
    throw new Error("model must be a non-empty string")
  }
  if (typeof text !== "string" || text.length === 0) {
    throw new Error("input must be a non-empty string")
  }
  if (typeof voice !== "string" || voice.length === 0) {
    throw new Error("voice must be a non-empty string")
  }
  const body: Record<string, unknown> = { model, input: text, voice }
  if (responseFormat !== undefined) body.response_format = responseFormat
  if (speed !== undefined) body.speed = speed
  if (instructions !== undefined) body.instructions = instructions
  if (extra !== undefined) {
    for (const [k, v] of Object.entries(extra)) body[k] = v
  }
  return rawRequest("POST", "/audio/speech", body, http)
}

/** Transcribe an audio file with OpenAI-compatible multipart upload. */
export async function transcribe(input: TranscriptionOptions): Promise<Response> {
  return sttRequest("/audio/transcriptions", input)
}

/** Translate an audio file to English (Whisper translation endpoint). */
export async function audioTranslate(
  input: TranscriptionOptions,
): Promise<Response> {
  return sttRequest("/audio/translations", input)
}

async function sttRequest(
  path: string,
  input: TranscriptionOptions,
): Promise<Response> {
  const {
    model,
    file,
    filename,
    language,
    prompt,
    responseFormat,
    temperature,
    extra,
    ...http
  } = input
  if (typeof model !== "string" || model.length === 0) {
    throw new Error("model must be a non-empty string")
  }
  if (!(file instanceof Blob)) {
    throw new Error("file must be a Blob or File")
  }
  const form = new FormData()
  form.append("model", model)
  form.append("file", file, filename)
  if (language !== undefined) form.append("language", language)
  if (prompt !== undefined) form.append("prompt", prompt)
  if (responseFormat !== undefined) form.append("response_format", responseFormat)
  if (temperature !== undefined) form.append("temperature", String(temperature))
  if (extra !== undefined) {
    for (const [k, v] of Object.entries(extra)) form.append(k, v)
  }
  return rawBodyRequest("POST", path, form, http)
}

// ---------- images: edits + variations ----------

export interface ImageEditOptions extends HttpOptions {
  model: string
  image: Blob
  prompt: string
  mask?: Blob
  imageFilename?: string
  maskFilename?: string
  n?: number
  size?: string
  responseFormat?: "url" | "b64_json"
  extra?: Record<string, string | Blob>
}

export interface ImageVariationOptions extends HttpOptions {
  model: string
  image: Blob
  imageFilename?: string
  n?: number
  size?: string
  responseFormat?: "url" | "b64_json"
  extra?: Record<string, string | Blob>
}

/** Edit an image with an OpenAI-compatible multipart upload. */
export async function imageEdit(input: ImageEditOptions): Promise<Response> {
  const {
    model,
    image,
    prompt,
    mask,
    imageFilename,
    maskFilename,
    n,
    size,
    responseFormat,
    extra,
    ...http
  } = input
  if (typeof model !== "string" || model.length === 0) {
    throw new Error("model must be a non-empty string")
  }
  if (typeof prompt !== "string" || prompt.length === 0) {
    throw new Error("prompt must be a non-empty string")
  }
  if (!(image instanceof Blob)) {
    throw new Error("image must be a Blob or File")
  }
  const form = new FormData()
  form.append("model", model)
  form.append("prompt", prompt)
  form.append("image", image, imageFilename ?? "image.png")
  if (mask !== undefined) {
    if (!(mask instanceof Blob)) {
      throw new Error("mask must be a Blob or File")
    }
    form.append("mask", mask, maskFilename ?? "mask.png")
  }
  if (n !== undefined) form.append("n", String(n))
  if (size !== undefined) form.append("size", size)
  if (responseFormat !== undefined)
    form.append("response_format", responseFormat)
  if (extra !== undefined) {
    for (const [k, v] of Object.entries(extra)) form.append(k, v)
  }
  return rawBodyRequest("POST", "/images/edits", form, http)
}

/** Generate variations of an image with an OpenAI-compatible multipart upload. */
export async function imageVariations(
  input: ImageVariationOptions,
): Promise<Response> {
  const {
    model,
    image,
    imageFilename,
    n,
    size,
    responseFormat,
    extra,
    ...http
  } = input
  if (typeof model !== "string" || model.length === 0) {
    throw new Error("model must be a non-empty string")
  }
  if (!(image instanceof Blob)) {
    throw new Error("image must be a Blob or File")
  }
  const form = new FormData()
  form.append("model", model)
  form.append("image", image, imageFilename ?? "image.png")
  if (n !== undefined) form.append("n", String(n))
  if (size !== undefined) form.append("size", size)
  if (responseFormat !== undefined)
    form.append("response_format", responseFormat)
  if (extra !== undefined) {
    for (const [k, v] of Object.entries(extra)) form.append(k, v)
  }
  return rawBodyRequest("POST", "/images/variations", form, http)
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
  image,
  speech,
  transcribe,
  audioTranslate,
  imageEdit,
  imageVariations,
  models,
  cacheStats,
} as const

/** `suite.audio.*` — TTS + STT verbs.
 *
 * Returns raw `Response` objects so callers can stream bytes (TTS) or
 * read JSON (STT). For multimodal-only callers that want a tighter
 * surface, this namespace is the entry point — `suite.llm.*` aliases the
 * same functions for OpenAI-SDK-style consumers.
 */
export const audio = {
  speech,
  transcribe,
  translate: audioTranslate,
} as const

/** `suite.images.*` — image generation, edit, variations. */
export const images = {
  generate: image,
  edit: imageEdit,
  variations: imageVariations,
} as const
