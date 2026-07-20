// SPDX-License-Identifier: Apache-2.0

// Explicit, configurable BackAI client — the TypeScript counterpart to the
// env-configured `suite.*` singleton and to `af_stack.BackAI` in Python.
//
//   import { BackAI } from "@af-stack/sdk"
//
//   const client = new BackAI({ baseUrl: "https://api.example.com", apiKey: "sk-..." })
//   const result = await client.agents.call("supportdesk.echo", { payload: { m: "hi" } })
//
// Instead of reading `AF_STACK_URL` / `AF_STACK_API_KEY` from the environment,
// it takes explicit `{ baseUrl, apiKey, timeout, maxRetries }` and exposes the
// same operational namespaces. Every namespace method is transparently bound
// to the client's config for the duration of the call via an ambient
// `ClientConfig` slot (the direct analogue of `af_stack`'s contextvar
// transport), so the existing namespace modules target the client's base URL /
// key / retry policy with no per-module rewiring. The env-configured singleton
// path is unchanged.
//
// The exposed namespaces + methods are the cross-language parity contract in
// `packages/sdk-parity.json`; the parity test asserts this client's surface
// matches the manifest exactly, so a method added to only one SDK fails tests.

import {
  checkRuntimeCompat,
  DEFAULT_MAX_RETRIES,
  DEFAULT_TIMEOUT_MS,
  request,
  runGenWithConfig,
  runWithConfig,
  SuiteError,
  type ClientConfig,
} from "./_http.js"

import { agents } from "./agents.js"
import { approvals } from "./approvals.js"
import { audio, images, llm } from "./llm.js"
import { auth } from "./auth.js"
import { billing } from "./billing.js"
import { cost } from "./cost.js"
import { harnesses } from "./harnesses.js"
import { jobs } from "./jobs.js"
import { memory } from "./memory.js"
import { notifications } from "./notifications.js"
import { oauth } from "./oauth.js"
import { realtime } from "./realtime.js"
import { runs } from "./runs.js"
import { sandbox } from "./sandbox.js"
import { searchIndex } from "./search.js"
import { shipwright } from "./shipwright.js"
import { storage } from "./storage.js"
import { tools } from "./tools.js"
import { webhooks } from "./webhooks.js"

/**
 * The governed namespaces exposed by the explicit client, sorted. MUST agree
 * with `packages/sdk-parity.json` (enforced by tests/parity.test.ts).
 */
export const GOVERNED_NAMESPACES = [
  "agents",
  "approvals",
  "audio",
  "auth",
  "billing",
  "cost",
  "harnesses",
  "images",
  "jobs",
  "llm",
  "memory",
  "notifications",
  "oauth",
  "realtime",
  "runs",
  "sandbox",
  "search",
  "shipwright",
  "storage",
  "tools",
  "webhooks",
] as const

export interface BackAIOptions {
  /** Runtime base URL. Defaults to `AF_STACK_URL` (`http://localhost:8080`). */
  baseUrl?: string
  /** Bearer key. Defaults to `AF_STACK_API_KEY`. */
  apiKey?: string
  /** Per-request timeout in milliseconds (default 30_000). */
  timeout?: number
  /**
   * Automatic retries for transient failures (429/5xx). Defaults to 2. Only
   * safe methods (GET/HEAD/OPTIONS) retry automatically; mutating calls retry
   * solely when an `idempotencyKey` option is supplied.
   */
  maxRetries?: number
  /**
   * When true (default), lazily fetch `GET /version` once and `console.warn`
   * on a major version mismatch. Tolerates a 404.
   */
  checkRuntimeVersion?: boolean
}

function isAsyncGeneratorFn(fn: unknown): boolean {
  return (
    typeof fn === "function" &&
    (fn as { constructor?: { name?: string } }).constructor?.name === "AsyncGeneratorFunction"
  )
}

/**
 * Wrap `fn` so it runs against `config`'s ambient binding. Async generators
 * hold the binding for the lifetime of the returned iterable; everything else
 * (async functions, and the synchronous WebSocket `subscribe` helpers) reads
 * the binding during its synchronous prelude, before its first `await`.
 */
function bindMethod<F extends (...args: never[]) => unknown>(
  fn: F,
  config: ClientConfig,
  onCall: () => void,
): F {
  const invoke = fn as unknown as (...a: unknown[]) => unknown
  if (isAsyncGeneratorFn(fn)) {
    return ((...args: unknown[]) => {
      onCall()
      return runGenWithConfig(config, () => invoke(...args) as AsyncIterable<unknown>)
    }) as unknown as F
  }
  return ((...args: unknown[]) => {
    onCall()
    return runWithConfig(config, () => invoke(...args))
  }) as unknown as F
}

/** Bind every method of a namespace object to `config`, preserving its type. */
function bindNamespace<NS extends Record<string, (...args: never[]) => unknown>>(
  ns: NS,
  config: ClientConfig,
  onCall: () => void,
): NS {
  const out: Record<string, unknown> = {}
  for (const key of Object.keys(ns)) {
    out[key] = bindMethod(ns[key], config, onCall)
  }
  return out as NS
}

/**
 * Bind only the named subset of a namespace object. Used where the module
 * object exposes MORE than the parity contract governs — `llm` also carries
 * the `audio`/`images` helpers, and `runs` carries `subscribeById` — so the
 * governed client namespace stays exactly the manifest's method set.
 */
function bindPicked<
  NS extends Record<string, (...args: never[]) => unknown>,
  K extends keyof NS & string,
>(ns: NS, keys: readonly K[], config: ClientConfig, onCall: () => void): Pick<NS, K> {
  const out: Partial<Pick<NS, K>> = {}
  for (const key of keys) {
    out[key] = bindMethod(ns[key], config, onCall)
  }
  return out as Pick<NS, K>
}

const LLM_METHODS = ["cacheStats", "chat", "embed", "models"] as const
const RUNS_METHODS = ["subscribe"] as const

/** An explicit BackAI suite client. */
export class BackAI {
  readonly agents: typeof agents
  readonly approvals: typeof approvals
  readonly audio: typeof audio
  readonly auth: typeof auth
  readonly billing: typeof billing
  readonly cost: typeof cost
  readonly harnesses: typeof harnesses
  readonly images: typeof images
  readonly jobs: typeof jobs
  readonly llm: Pick<typeof llm, (typeof LLM_METHODS)[number]>
  readonly memory: typeof memory
  readonly notifications: typeof notifications
  readonly oauth: typeof oauth
  readonly realtime: typeof realtime
  readonly runs: Pick<typeof runs, (typeof RUNS_METHODS)[number]>
  readonly sandbox: typeof sandbox
  readonly search: typeof searchIndex
  readonly shipwright: typeof shipwright
  readonly storage: typeof storage
  readonly tools: typeof tools
  readonly webhooks: typeof webhooks

  private readonly config: ClientConfig
  private readonly checkVersion: boolean
  private versionCheckStarted = false

  constructor(opts: BackAIOptions = {}) {
    // baseUrl / apiKey are left undefined when not supplied so the transport
    // falls back to AF_STACK_URL / AF_STACK_API_KEY (then the built-in
    // default). timeout / maxRetries always carry the client defaults so they
    // apply to every delegated call.
    this.config = {
      baseUrl: opts.baseUrl,
      apiKey: opts.apiKey,
      timeout: opts.timeout ?? DEFAULT_TIMEOUT_MS,
      maxRetries: opts.maxRetries ?? DEFAULT_MAX_RETRIES,
    }
    this.checkVersion = opts.checkRuntimeVersion ?? true

    const onCall = (): void => {
      void this.ensureVersionChecked()
    }

    this.agents = bindNamespace(agents, this.config, onCall)
    this.approvals = bindNamespace(approvals, this.config, onCall)
    this.audio = bindNamespace(audio, this.config, onCall)
    this.auth = bindNamespace(auth, this.config, onCall)
    this.billing = bindNamespace(billing, this.config, onCall)
    this.cost = bindNamespace(cost, this.config, onCall)
    this.harnesses = bindNamespace(harnesses, this.config, onCall)
    this.images = bindNamespace(images, this.config, onCall)
    this.jobs = bindNamespace(jobs, this.config, onCall)
    this.llm = bindPicked(llm, LLM_METHODS, this.config, onCall)
    this.memory = bindNamespace(memory, this.config, onCall)
    this.notifications = bindNamespace(notifications, this.config, onCall)
    this.oauth = bindNamespace(oauth, this.config, onCall)
    this.realtime = bindNamespace(realtime, this.config, onCall)
    this.runs = bindPicked(runs, RUNS_METHODS, this.config, onCall)
    this.sandbox = bindNamespace(sandbox, this.config, onCall)
    this.search = bindNamespace(searchIndex, this.config, onCall)
    this.shipwright = bindNamespace(shipwright, this.config, onCall)
    this.storage = bindNamespace(storage, this.config, onCall)
    this.tools = bindNamespace(tools, this.config, onCall)
    this.webhooks = bindNamespace(webhooks, this.config, onCall)
  }

  /** The base URL this client resolves against (undefined ⇒ env / default). */
  get baseUrl(): string | undefined {
    return this.config.baseUrl
  }

  /** The automatic retry budget for transient failures. */
  get maxRetries(): number {
    return this.config.maxRetries ?? 0
  }

  /**
   * Fetch the runtime version, or `null` when the endpoint is absent (404) or
   * unreachable. Never throws — the probe is best-effort.
   */
  async runtimeVersion(): Promise<string | null> {
    let body: unknown
    try {
      body = await runWithConfig(this.config, () => request<unknown>("GET", "/version", null, {}))
    } catch (err) {
      if (err instanceof SuiteError && err.status === 404) return null
      return null
    }
    if (body !== null && typeof body === "object") {
      const rec = body as Record<string, unknown>
      const raw = rec.version ?? rec.runtime_version
      return raw !== undefined && raw !== null ? String(raw) : null
    }
    return null
  }

  /**
   * Fetch the runtime version once and `console.warn` on a major mismatch.
   * Idempotent (runs at most once) and best-effort (never throws). Fired
   * lazily on the first namespace call; also callable directly (e.g. tests).
   */
  async ensureVersionChecked(): Promise<void> {
    if (!this.checkVersion || this.versionCheckStarted) return
    this.versionCheckStarted = true
    let version: string | null = null
    try {
      version = await this.runtimeVersion()
    } catch {
      version = null
    }
    const message = checkRuntimeCompat(version)
    if (message !== null) console.warn(message)
  }

  /** Release any client resources. Provided for symmetry with `af_stack`. */
  async close(): Promise<void> {
    // The fetch-based transport holds no persistent connection to close.
  }
}
