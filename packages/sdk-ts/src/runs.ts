// SPDX-License-Identifier: Apache-2.0

// suite.runs.* — live AgentField run event subscriptions.
//
// Two entry points:
//
//   suite.runs.subscribeById(runId)   — proxy AF's per-run SSE stream
//                                       (existing, kept for back-compat)
//   suite.runs.subscribe({ ... })     — #15: filter-based subscription
//                                       (tenant / user / agent / run / exec)
//
// Both return a raw WebSocket the caller drives. The Python SDK exposes
// async iterators because that's the Python idiom; here we expose
// WebSockets so apps can hook them into React state, RxJS, etc.

import { resolveApiKey, resolveBaseUrl, type HttpOptions } from "./_http.js"

// ─── existing per-run shape (kept for back-compat) ────────────────────────

export interface AgentRunEvent<TData = unknown> {
  type: "agentfield.run_event" | "heartbeat" | string
  event?: string
  data?: TData
  raw?: string
  path?: string
}

export interface SubscribeRunOptions extends HttpOptions {
  /**
   * WebSocket constructor override for runtimes that do not expose a global
   * WebSocket. Browsers and most edge runtimes do not need this.
   */
  WebSocket?: typeof WebSocket
}

function websocketBase(baseUrl: string): string {
  const url = new URL(baseUrl)
  if (url.protocol === "https:") url.protocol = "wss:"
  else url.protocol = "ws:"
  return url.toString().replace(/\/+$/, "")
}

/**
 * Subscribe to AgentField-owned run events for a single run id, via the
 * AF Stack gateway. This is the existing per-run proxy at
 * `GET /api/v1/runs/{id}/events` (sets up an SSE-to-WebSocket relay
 * server-side). Use {@link subscribe} for filter-based subscriptions.
 */
export function subscribeById(runId: string, opts: SubscribeRunOptions = {}): WebSocket {
  if (runId.trim() === "") {
    throw new Error("runId is required")
  }
  const WebSocketCtor = opts.WebSocket ?? globalThis.WebSocket
  if (WebSocketCtor === undefined) {
    throw new Error("globalThis.WebSocket is not available; pass opts.WebSocket")
  }

  const url = new URL(
    `${websocketBase(resolveBaseUrl(opts.baseUrl))}/api/v1/runs/${encodeURIComponent(runId)}/events`,
  )
  const apiKey = resolveApiKey(opts.apiKey)
  if (apiKey !== undefined) {
    url.searchParams.set("api_key", apiKey)
  }

  const socket = new WebSocketCtor(url.toString())
  if (opts.signal !== undefined) {
    if (opts.signal.aborted) {
      socket.close()
    } else {
      opts.signal.addEventListener("abort", () => socket.close(), { once: true })
    }
  }
  return socket
}

// ─── #15 filter-based subscription ────────────────────────────────────────

/** Wire-protocol event type. Matches services/runtime/internal/server/runs.go. */
export type RunEventType =
  | "run.started"
  | "run.step"
  | "run.completed"
  | "run.error"
  | "server.degraded"

/** Live agent run event. Optional fields are populated when AgentField's
 * underlying event carried the value; `raw` always carries the original
 * payload for debug introspection.
 */
export interface RunEvent {
  type: RunEventType
  run_id?: string
  execution_id?: string
  agent?: string
  node_id?: string
  step_id?: string
  reasoner?: string
  status?: string
  started_at?: string
  ended_at?: string
  updated_at?: string
  duration_ms?: number
  cost_usd?: number
  tool_calls?: unknown[]
  tokens?: Record<string, unknown>
  error?: string
  reason?: string
  raw?: Record<string, unknown>
}

/** Server-side filter set. All fields optional. Tenant id is auto-bound
 *  to the caller's resolved tenant when multi-tenancy is enabled on the
 *  runtime — any value passed here is overwritten.
 */
export interface RunsSubscribeFilter {
  tenant_id?: string
  user_id?: string
  agent?: string
  run_id?: string
  execution_id?: string
}

/**
 * Subscribe to live AgentField run events with server-side filtering.
 * Returns a raw WebSocket whose `message` events carry JSON-encoded
 * {@link RunEvent} envelopes.
 *
 * ```ts
 * const ws = suite.runs.subscribe({ run_id: "run-1" })
 * ws.addEventListener("message", (e) => {
 *   const evt: RunEvent = JSON.parse(e.data)
 *   // evt.type === "run.started" | "run.step" | "run.completed" | ...
 * })
 * ```
 *
 * The runtime emits a `{"type":"server.degraded"}` envelope and closes
 * the socket when AgentField is unreachable — treat it as a transient
 * failure and reconnect with backoff.
 */
export function subscribe(
  filter: RunsSubscribeFilter = {},
  opts: SubscribeRunOptions = {},
): WebSocket {
  const WebSocketCtor = opts.WebSocket ?? globalThis.WebSocket
  if (WebSocketCtor === undefined) {
    throw new Error("globalThis.WebSocket is not available; pass opts.WebSocket")
  }

  const url = new URL(`${websocketBase(resolveBaseUrl(opts.baseUrl))}/api/v1/realtime/runs`)
  if (filter.tenant_id !== undefined && filter.tenant_id !== "") {
    url.searchParams.set("tenant_id", filter.tenant_id)
  }
  if (filter.user_id !== undefined && filter.user_id !== "") {
    url.searchParams.set("user_id", filter.user_id)
  }
  if (filter.agent !== undefined && filter.agent !== "") {
    url.searchParams.set("agent", filter.agent)
  }
  if (filter.run_id !== undefined && filter.run_id !== "") {
    url.searchParams.set("run_id", filter.run_id)
  }
  if (filter.execution_id !== undefined && filter.execution_id !== "") {
    url.searchParams.set("execution_id", filter.execution_id)
  }
  const apiKey = resolveApiKey(opts.apiKey)
  if (apiKey !== undefined) {
    url.searchParams.set("api_key", apiKey)
  }

  const socket = new WebSocketCtor(url.toString())
  if (opts.signal !== undefined) {
    if (opts.signal.aborted) {
      socket.close()
    } else {
      opts.signal.addEventListener("abort", () => socket.close(), { once: true })
    }
  }
  return socket
}

export const runs = {
  /** #15 filter-based subscription. */
  subscribe,
  /** Legacy per-run proxy (kept so older callers keep compiling). */
  subscribeById,
}
