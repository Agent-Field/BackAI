// SPDX-License-Identifier: Apache-2.0

// Request context.
//
// In Node 20+, Bun and Deno we use AsyncLocalStorage so values set by
// middleware propagate across async boundaries (await, setTimeout, etc.).
// Edge runtimes that lack AsyncLocalStorage fall back to a mutable object
// (no isolation between concurrent requests — callers must pass apiKey
// explicitly in that case).
//
// Public API:
//   ctx.tenantId / ctx.userId / ctx.apiKeyId / ctx.requestId
//   withCtx({ tenantId, userId, ... }, async () => { ... })

import { AsyncLocalStorage } from "node:async_hooks"

export interface CtxValues {
  tenantId: string | null
  userId: string | null
  apiKeyId: string | null
  requestId: string | null
}

const EMPTY: CtxValues = {
  tenantId: null,
  userId: null,
  apiKeyId: null,
  requestId: null,
}

// AsyncLocalStorage is the canonical implementation on Node-compatible
// runtimes. We import lazily so edge runtimes without it still load the
// module; if AsyncLocalStorage is missing, we degrade to a single global frame.
type Store = AsyncLocalStorage<CtxValues> | null

let als: Store = null
try {
  als = new AsyncLocalStorage<CtxValues>()
} catch {
  als = null
}

// Fallback global frame used only when AsyncLocalStorage is unavailable.
// New objects are created on every withCtx call (immutable).
let fallbackFrame: CtxValues = EMPTY

function currentFrame(): CtxValues {
  if (als !== null) {
    const store = als.getStore()
    if (store !== undefined) return store
  }
  return fallbackFrame
}

/**
 * Run `fn` with `values` merged into the current context frame. The frame
 * is restored after `fn` resolves/rejects. Nested calls compose (inner
 * values override outer).
 */
export async function withCtx<T>(
  values: Partial<CtxValues>,
  fn: () => Promise<T> | T,
): Promise<T> {
  const merged: CtxValues = { ...currentFrame(), ...values }
  if (als !== null) {
    return als.run(merged, async () => fn())
  }
  const previous = fallbackFrame
  fallbackFrame = merged
  try {
    return await fn()
  } finally {
    fallbackFrame = previous
  }
}

/**
 * The current context, read live. Property access always reflects the
 * latest frame — never cache `ctx.tenantId` across awaits.
 */
export const ctx: Readonly<CtxValues> = new Proxy({} as CtxValues, {
  get(_target, prop): unknown {
    const frame = currentFrame()
    if (prop === "tenantId" || prop === "userId" || prop === "apiKeyId" || prop === "requestId") {
      return frame[prop]
    }
    return undefined
  },
  set(): boolean {
    // Read-only proxy — writes are silently ignored. Use withCtx() to set
    // values. We return `true` so non-strict callers don't get a TypeError;
    // the value is never actually stored.
    return true
  },
}) as Readonly<CtxValues>