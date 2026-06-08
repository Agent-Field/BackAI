// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  chat,
  embed,
  llm,
  llmCacheStats,
  llmModels,
  suite,
  llmImage,
  llmSpeech,
  llmTranscribe,
} from "../src/index.js"

let fetchMock: ReturnType<typeof vi.fn>
let responseQueue: Response[]

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
    ...init,
  })
}

function sseResponse(chunks: string[]): Response {
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      const encoder = new TextEncoder()
      for (const c of chunks) controller.enqueue(encoder.encode(c))
      controller.close()
    },
  })
  return new Response(stream, {
    status: 200,
    headers: { "content-type": "text/event-stream" },
  })
}

function enqueueResponse(response: Response): void {
  responseQueue.push(response)
}

interface MockCall { url: string; init: RequestInit }
function nthCall(idx: number): MockCall {
  const args = fetchMock.mock.calls[idx]
  if (args === undefined) throw new Error(`no fetch call at idx ${idx}`)
  return { url: String(args[0]), init: (args[1] as RequestInit | undefined) ?? {} }
}

function headersOf(init: RequestInit): Record<string, string> {
  const h = init.headers
  if (h === undefined) return {}
  if (h instanceof Headers) {
    const out: Record<string, string> = {}
    h.forEach((v, k) => {
      out[k.toLowerCase()] = v
    })
    return out
  }
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(h as Record<string, string>)) {
    out[k.toLowerCase()] = v
  }
  return out
}

beforeEach(() => {
  responseQueue = []
  fetchMock = vi.fn(async () => responseQueue.shift() ?? jsonResponse({}))
  ;(globalThis as { fetch: typeof fetch }).fetch = fetchMock as unknown as typeof fetch
  process.env.AF_STACK_URL = "http://test.local"
  process.env.AF_STACK_API_KEY = "tk_llm_test"
})

afterEach(() => {
  vi.restoreAllMocks()
  delete process.env.AF_STACK_URL
  delete process.env.AF_STACK_API_KEY
})

// ─── chat (non-streaming) ────────────────────────────────────────────────

describe("chat (non-streaming)", () => {
  it("POSTs OpenAI-shaped body and returns the Response", async () => {
    enqueueResponse(jsonResponse({
      id: "chatcmpl-abc123",
      model: "qwen/qwen-2.5-72b-instruct",
      object: "chat.completion",
      choices: [
        {
          index: 0,
          message: { role: "assistant", content: "Hello back!" },
          finish_reason: "stop",
        },
      ],
      usage: { prompt_tokens: 10, completion_tokens: 4, total_tokens: 14 },
    }))
    const response = await chat({
      model: "qwen/qwen-2.5-72b-instruct",
      messages: [{ role: "user", content: "Hello" }],
      temperature: 0.7,
    })
    expect(response.status).toBe(200)
    const body = (await response.json()) as { choices: Array<{ message: { content: string } }> }
    expect(body.choices[0].message.content).toBe("Hello back!")

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/llm/chat/completions")
    expect(c.init.method).toBe("POST")
    const reqBody = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(reqBody.model).toBe("qwen/qwen-2.5-72b-instruct")
    expect(reqBody.temperature).toBe(0.7)
    expect((reqBody.messages as unknown[]).length).toBe(1)

    // Auth header forwarded from env.
    const h = headersOf(c.init)
    expect(h.authorization).toBe("Bearer tk_llm_test")
  })

  it("translates camelCase options to snake_case wire fields", async () => {
    enqueueResponse(jsonResponse({}))
    await chat({
      model: "m",
      messages: [{ role: "user", content: "x" }],
      maxTokens: 100,
      topP: 0.9,
    })
    const c = nthCall(0)
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.max_tokens).toBe(100)
    expect(body.top_p).toBe(0.9)
  })

  it("rejects empty model", async () => {
    await expect(
      chat({ model: "", messages: [{ role: "user", content: "hi" }] }),
    ).rejects.toThrow(/model/)
  })

  it("rejects empty messages", async () => {
    await expect(chat({ model: "m", messages: [] })).rejects.toThrow(/messages/)
  })
})

// ─── chat (streaming) ────────────────────────────────────────────────────

describe("chat (streaming)", () => {
  it("yields decoded chunks until [DONE]", async () => {
    enqueueResponse(sseResponse([
      'data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hel"}}]}\n\n',
      'data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"lo"}}]}\n\n',
      'data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"!"}}]}\n\n',
      'data: [DONE]\n\n',
    ]))
    const iterator = await chat({
      model: "qwen/qwen-2.5-72b-instruct",
      messages: [{ role: "user", content: "Hi" }],
      stream: true,
    })
    const pieces: string[] = []
    for await (const chunk of iterator) {
      const text = chunk.choices?.[0]?.delta?.content
      if (typeof text === "string") pieces.push(text)
    }
    expect(pieces.join("")).toBe("Hello!")

    // Streaming request must send stream:true and the SSE accept header.
    const c = nthCall(0)
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.stream).toBe(true)
    const h = headersOf(c.init)
    expect(h.accept).toBe("text/event-stream")
  })
})

// ─── embed ───────────────────────────────────────────────────────────────

describe("embed", () => {
  it("POSTs body and returns the Response", async () => {
    enqueueResponse(jsonResponse({
      model: "text-embedding-3-small",
      data: [{ index: 0, embedding: [0.1, 0.2, 0.3] }],
      usage: { prompt_tokens: 4, total_tokens: 4 },
    }))
    const response = await embed({
      model: "text-embedding-3-small",
      input: "hello world",
    })
    const body = (await response.json()) as {
      data: Array<{ embedding: number[] }>
    }
    expect(body.data[0].embedding).toEqual([0.1, 0.2, 0.3])

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/embeddings")
    const reqBody = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(reqBody.model).toBe("text-embedding-3-small")
    expect(reqBody.input).toBe("hello world")
  })

  it("accepts batch input", async () => {
    enqueueResponse(jsonResponse({ data: [] }))
    await embed({ model: "m", input: ["a", "b", "c"] })
    const c = nthCall(0)
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.input).toEqual(["a", "b", "c"])
  })

  it("rejects empty input array", async () => {
    await expect(embed({ model: "m", input: [] })).rejects.toThrow(/input/)
  })
})

// ─── multimodal ──────────────────────────────────────────────────────────

describe("image", () => {
  it("POSTs to the top-level images endpoint", async () => {
    enqueueResponse(jsonResponse({ data: [{ url: "https://example.test/image.png" }] }))
    await llmImage({
      prompt: "a product diagram",
      model: "gpt-image-1",
      responseFormat: "url",
    })
    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/images/generations")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.prompt).toBe("a product diagram")
    expect(body.model).toBe("gpt-image-1")
    expect(body.response_format).toBe("url")
  })
})

describe("speech", () => {
  it("POSTs to the audio speech endpoint and returns raw response", async () => {
    enqueueResponse(new Response("mp3 bytes", {
      status: 200,
      headers: { "content-type": "audio/mpeg" },
    }))
    const response = await llmSpeech({
      model: "tts-1",
      input: "hello",
      voice: "alloy",
      responseFormat: "mp3",
    })
    expect(await response.text()).toBe("mp3 bytes")

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/audio/speech")
    const body = JSON.parse(c.init.body as string) as Record<string, unknown>
    expect(body.model).toBe("tts-1")
    expect(body.input).toBe("hello")
    expect(body.voice).toBe("alloy")
    expect(body.response_format).toBe("mp3")
  })
})

describe("transcribe", () => {
  it("POSTs multipart form data to the audio transcription endpoint", async () => {
    enqueueResponse(jsonResponse({ text: "hello" }))
    const file = new Blob(["audio bytes"], { type: "audio/mpeg" })
    await llmTranscribe({
      model: "whisper-1",
      file,
      filename: "clip.mp3",
      language: "en",
    })

    const c = nthCall(0)
    expect(c.url).toBe("http://test.local/api/v1/audio/transcriptions")
    expect(c.init.body).toBeInstanceOf(FormData)
    const h = headersOf(c.init)
    expect(h["content-type"]).toBeUndefined()
    const form = c.init.body as FormData
    expect(form.get("model")).toBe("whisper-1")
    expect(form.get("language")).toBe("en")
    expect(form.get("file")).toBeInstanceOf(Blob)
  })
})

// ─── models / cacheStats ─────────────────────────────────────────────────

describe("models", () => {
  it("GETs /llm/models and parses to camelCase", async () => {
    enqueueResponse(jsonResponse({
      models: [
        {
          id: "qwen/qwen-2.5-72b-instruct",
          display_name: "Qwen 2.5 72B Instruct",
          provider: "openrouter",
          prompt_usd_per_1m: 0.3,
          completion_usd_per_1m: 0.3,
          supports_streaming: true,
          supports_tools: true,
        },
      ],
    }))
    const result = await llmModels()
    expect(result.models[0].id).toBe("qwen/qwen-2.5-72b-instruct")
    expect(result.models[0].displayName).toBe("Qwen 2.5 72B Instruct")
    expect(result.models[0].supportsStreaming).toBe(true)
  })
})

describe("cacheStats", () => {
  it("GETs /llm/cache/stats and parses to camelCase", async () => {
    enqueueResponse(jsonResponse({
      total_calls: 100,
      cache_hits: 30,
      cache_misses: 70,
      hit_rate: 0.3,
      savings_usd: 0.05,
      entries: 12,
    }))
    const result = await llmCacheStats()
    expect(result.totalCalls).toBe(100)
    expect(result.hitRate).toBe(0.3)
    expect(result.savingsUsd).toBe(0.05)
  })
})

// ─── namespace ────────────────────────────────────────────────────────────

describe("namespace exports", () => {
  it("suite.llm matches the module helpers", () => {
    expect(suite.llm).toBe(llm)
    expect(suite.llm.chat).toBe(llm.chat)
    expect(suite.llm.models).toBe(llm.models)
    expect(suite.llm.image).toBe(llm.image)
    expect(suite.llm.speech).toBe(llm.speech)
    expect(suite.llm.transcribe).toBe(llm.transcribe)
  })
})
