// SPDX-License-Identifier: Apache-2.0

// suite.realtime.* — Postgres LISTEN/NOTIFY backed realtime subscriptions.

import { resolveApiKey, resolveBaseUrl, type HttpOptions } from "./_http.js"

export type RealtimeFilter = Record<string, string | number | boolean | null>

export interface RealtimeEvent<TRecord = Record<string, unknown>> {
  table: string
  op?: "insert" | "update" | "delete" | string
  tenant_id?: string
  record?: TRecord
  old?: TRecord
  at?: string
  [key: string]: unknown
}

export interface SubscribeOptions extends HttpOptions {
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

export function subscribe(
  table: string,
  filter: RealtimeFilter = {},
  opts: SubscribeOptions = {},
): WebSocket {
  if (table.trim() === "") {
    throw new Error("table is required")
  }
  const WebSocketCtor = opts.WebSocket ?? globalThis.WebSocket
  if (WebSocketCtor === undefined) {
    throw new Error("globalThis.WebSocket is not available; pass opts.WebSocket")
  }

  const url = new URL(`${websocketBase(resolveBaseUrl(opts.baseUrl))}/api/v1/realtime`)
  url.searchParams.set("table", table)
  if (Object.keys(filter).length > 0) {
    url.searchParams.set("filter", JSON.stringify(filter))
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

export const realtime = {
  subscribe,
}
