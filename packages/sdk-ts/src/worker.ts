// SPDX-License-Identifier: Apache-2.0

// `worker` — run language-neutral background jobs in TypeScript.
//
// A `Worker` is a long-running process that PULLS remote (typescript) job
// definitions from the BackAI runtime and executes them. It is the TypeScript
// half of the pull-based worker protocol (PRD R3) and mirrors
// `af_stack.worker` 1:1; the runtime side lives at `/api/v1/jobs/worker/*`.
//
//   import { Worker } from "@af-stack/sdk/server"
//
//   const worker = new Worker("http://localhost:8080", "af_live_...")
//
//   worker.register("resize-image", async (payload, ctx) => {
//     ctx.log("resizing", { fields: { url: payload.url } })
//     if (ctx.isCanceled()) return
//     return { thumbnail: await doResize(payload.url) }
//   })
//
//   await worker.run() // resolves on SIGTERM/SIGINT after a graceful drain
//
// Handlers receive `(payload, ctx)` where `ctx` is a `JobContext` carrying
// `tenantId`, `jobId`, `attempt`, `deadline`, a structured `log()` helper, and
// `isCanceled()`.
//
// Failure semantics (identical to the Python worker):
//   * a handler that returns normally  -> the job COMPLETES with its result.
//   * a handler that throws PermanentError -> the job FAILS permanently
//     (River will not retry — it is dead-lettered).
//   * a handler that throws anything else -> the job FAILS *retryably*
//     (River retries with backoff, up to the job's max attempts).
//
// The worker authenticates with a tenant API key carrying the `jobs:work`
// scope.

const WORKER_PREFIX = "/api/v1/jobs/worker"

/** Handler signature: `(payload, ctx)` -> optional JSON-serialisable result. */
export type Handler = (
  payload: Record<string, unknown>,
  ctx: JobContext,
) => unknown | Promise<unknown>

/** Throw from a handler to fail the job WITHOUT a retry (dead-letter). */
export class PermanentError extends Error {
  constructor(message?: string) {
    super(message)
    this.name = "PermanentError"
  }
}

/** The wire shape of a leased attempt (snake_case, from the Go runtime). */
interface WireAttempt {
  job_id: string
  attempt: number
  kind: string
  payload: Record<string, unknown> | null
  tenant_id: string
  deadline: string | null
  lease_expires_at: string | null
}

/** One structured log line attached to a job run. */
export interface LogLine {
  level?: string
  fields?: Record<string, unknown>
}

/**
 * Per-job context handed to a handler. `isCanceled()` reflects the most recent
 * heartbeat: once the runtime reports the job cancelled, a well-behaved
 * handler should stop work and return promptly.
 */
export class JobContext {
  readonly tenantId: string
  readonly jobId: string
  readonly attempt: number
  readonly deadline: string | null

  /** @internal */
  _canceled = false
  private readonly worker: Worker

  constructor(init: {
    worker: Worker
    tenantId: string
    jobId: string
    attempt: number
    deadline: string | null
  }) {
    this.worker = init.worker
    this.tenantId = init.tenantId
    this.jobId = init.jobId
    this.attempt = init.attempt
    this.deadline = init.deadline
  }

  /** True once the runtime has reported this job cancelled. */
  isCanceled(): boolean {
    return this._canceled
  }

  /**
   * Attach a structured log line to this job's run. Best-effort: a transport
   * failure is logged locally and swallowed so a logging hiccup never fails
   * the job.
   */
  async log(message: string, opts: LogLine = {}): Promise<void> {
    await this.worker._sendLogs(this.jobId, this.attempt, [
      { level: opts.level ?? "info", message, fields: opts.fields ?? {} },
    ])
  }
}

export interface WorkerOptions {
  /** Seconds a lease is held before it must be renewed (default 30). */
  leaseTtl?: number
  /** Seconds between heartbeats while a handler runs (default 10). */
  heartbeatInterval?: number
  /** Long-poll seconds the runtime holds a lease request open (default 25). */
  pollWait?: number
  /** Stable id for this worker instance; a random one is generated if omitted. */
  workerId?: string
  /** `fetch` override (mainly for tests). */
  fetch?: typeof fetch
}

type FetchLike = typeof fetch

/** A pull-based remote job worker. */
export class Worker {
  readonly baseUrl: string
  readonly workerId: string
  readonly leaseTtl: number
  readonly heartbeatInterval: number
  readonly pollWait: number

  private readonly apiKey: string
  private readonly fetchFn: FetchLike
  private readonly handlers = new Map<string, Handler>()
  private stopped = false
  private signalHandlers: Array<[NodeJS.Signals, () => void]> = []

  constructor(baseUrl: string, apiKey: string, opts: WorkerOptions = {}) {
    if (!baseUrl) throw new Error("baseUrl is required")
    if (!apiKey) throw new Error("apiKey is required")
    this.baseUrl = baseUrl.replace(/\/+$/, "")
    this.apiKey = apiKey
    this.leaseTtl = opts.leaseTtl ?? 30
    this.heartbeatInterval = opts.heartbeatInterval ?? 10
    this.pollWait = opts.pollWait ?? 25
    this.workerId = opts.workerId ?? `ts-${randomId(12)}`
    const f = opts.fetch ?? (globalThis as { fetch?: FetchLike }).fetch
    if (f === undefined) throw new Error("globalThis.fetch is not available; pass opts.fetch")
    this.fetchFn = f
  }

  // ----- registration -------------------------------------------------

  /** Register `handler` as the handler for `kind`. */
  register(kind: string, handler: Handler): this {
    if (!kind) throw new Error("kind must be a non-empty string")
    this.handlers.set(kind, handler)
    return this
  }

  /** The kinds this worker will lease. */
  kinds(): string[] {
    return [...this.handlers.keys()]
  }

  // ----- lifecycle ----------------------------------------------------

  /** Ask the run loop to drain and exit after the current job. */
  stop(): void {
    this.stopped = true
  }

  /**
   * Block, leasing + executing jobs until `stop()` (or SIGTERM/SIGINT). On a
   * signal the worker stops leasing new work and resolves once the in-flight
   * job (if any) finishes — a graceful drain.
   */
  async run(opts: { installSignalHandlers?: boolean } = {}): Promise<void> {
    if (this.handlers.size === 0) {
      throw new Error("no handlers registered; call worker.register(...) first")
    }
    if (opts.installSignalHandlers ?? true) this.installSignalHandlers()
    try {
      while (!this.stopped) {
        const attempt = await this.leaseOnce()
        if (attempt === null) continue
        await this.process(attempt)
      }
    } finally {
      this.removeSignalHandlers()
    }
  }

  // ----- protocol steps (individually testable) -----------------------

  /** Long-poll for one attempt. Returns the attempt or null. */
  async leaseOnce(): Promise<WireAttempt | null> {
    const body = await this.post(
      "/lease",
      {
        kinds: this.kinds(),
        worker_id: this.workerId,
        lease_ttl_seconds: this.leaseTtl,
        wait_seconds: this.pollWait,
      },
      { timeoutMs: this.pollWait * 1000 + 10_000 },
    )
    if (body === null || typeof body !== "object") return null
    const job = (body as { job?: WireAttempt | null }).job
    return job ?? null
  }

  /** Run the handler for one leased attempt and report the outcome. */
  async process(attempt: WireAttempt): Promise<void> {
    const kind = String(attempt.kind ?? "")
    const jobId = String(attempt.job_id ?? "")
    const attNum = Number(attempt.attempt ?? 1)
    const handler = this.handlers.get(kind)
    if (handler === undefined) {
      // Leased a kind we can't run (shouldn't happen — we only ask for our
      // kinds). Fail retryably so another worker can pick it up.
      await this.fail(jobId, attNum, `no handler for kind ${JSON.stringify(kind)}`, true)
      return
    }

    const ctx = new JobContext({
      worker: this,
      tenantId: String(attempt.tenant_id ?? ""),
      jobId,
      attempt: attNum,
      deadline: attempt.deadline ?? null,
    })

    const heartbeat = this.startHeartbeat(ctx)
    let result: unknown
    try {
      result = await handler(attempt.payload ?? {}, ctx)
    } catch (err) {
      heartbeat.stop()
      if (err instanceof PermanentError) {
        await this.fail(jobId, attNum, err.message || "permanent failure", false)
        return
      }
      const msg = err instanceof Error ? err.message : String(err)
      await this.fail(jobId, attNum, msg || "handler error", true)
      return
    } finally {
      heartbeat.stop()
    }

    if (ctx.isCanceled()) {
      // The runtime already finalised the job as cancelled; reporting a result
      // would be a no-op 409. Drop it.
      return
    }
    await this.complete(jobId, attNum, result)
  }

  private async complete(jobId: string, attempt: number, result: unknown): Promise<void> {
    await this.post(
      "/complete",
      {
        job_id: jobId,
        attempt,
        worker_id: this.workerId,
        result: result ?? {},
      },
      { swallow: true },
    )
  }

  private async fail(
    jobId: string,
    attempt: number,
    error: string,
    retryable: boolean,
  ): Promise<void> {
    await this.post(
      "/fail",
      {
        job_id: jobId,
        attempt,
        worker_id: this.workerId,
        error,
        retryable,
      },
      { swallow: true },
    )
  }

  /** Send one heartbeat. Returns true if the job was reported cancelled. */
  async sendHeartbeat(ctx: JobContext): Promise<boolean> {
    const body = await this.post(
      "/heartbeat",
      {
        job_id: ctx.jobId,
        attempt: ctx.attempt,
        worker_id: this.workerId,
        lease_ttl_seconds: this.leaseTtl,
      },
      { swallow: true },
    )
    const canceled =
      body !== null &&
      typeof body === "object" &&
      Boolean((body as { canceled?: boolean }).canceled)
    if (canceled) ctx._canceled = true
    return canceled
  }

  /** @internal — used by JobContext.log. */
  async _sendLogs(
    jobId: string,
    attempt: number,
    lines: Array<{ level: string; message: string; fields: Record<string, unknown> }>,
  ): Promise<void> {
    await this.post("/logs", { job_id: jobId, attempt, lines }, { swallow: true })
  }

  // ----- heartbeat ----------------------------------------------------

  private startHeartbeat(ctx: JobContext): { stop: () => void } {
    const intervalMs = Math.max(1, this.heartbeatInterval) * 1000
    const timer = setInterval(() => {
      void this.sendHeartbeat(ctx)
    }, intervalMs)
    // Don't keep the event loop alive purely for the heartbeat.
    ;(timer as { unref?: () => void }).unref?.()
    let stopped = false
    return {
      stop: () => {
        if (stopped) return
        stopped = true
        clearInterval(timer)
      },
    }
  }

  // ----- transport ----------------------------------------------------

  private async post(
    path: string,
    body: Record<string, unknown>,
    opts: { swallow?: boolean; timeoutMs?: number } = {},
  ): Promise<unknown> {
    const url = `${this.baseUrl}${WORKER_PREFIX}${path}`
    try {
      const init: RequestInit = {
        method: "POST",
        headers: {
          authorization: `Bearer ${this.apiKey}`,
          "content-type": "application/json",
          accept: "application/json",
        },
        body: JSON.stringify(body),
      }
      if (
        opts.timeoutMs !== undefined &&
        typeof AbortSignal !== "undefined" &&
        "timeout" in AbortSignal
      ) {
        init.signal = AbortSignal.timeout(opts.timeoutMs)
      }
      const response = await this.fetchFn(url, init)
      if (!response.ok) {
        throw new Error(`worker ${path} failed: HTTP ${response.status}`)
      }
      const text = await response.text()
      if (text === "") return null
      return JSON.parse(text)
    } catch (err) {
      if (opts.swallow) {
        // eslint-disable-next-line no-console
        console.warn(`[af-stack worker] request failed path=${path}:`, err)
        return null
      }
      throw err
    }
  }

  // ----- signals ------------------------------------------------------

  private installSignalHandlers(): void {
    const proc = (globalThis as { process?: NodeJS.Process }).process
    if (proc === undefined || typeof proc.on !== "function") return
    for (const sig of ["SIGTERM", "SIGINT"] as NodeJS.Signals[]) {
      const handler = (): void => {
        this.stop()
      }
      try {
        proc.on(sig, handler)
        this.signalHandlers.push([sig, handler])
      } catch {
        // Not permitted in this environment — skip.
      }
    }
  }

  private removeSignalHandlers(): void {
    const proc = (globalThis as { process?: NodeJS.Process }).process
    if (proc === undefined || typeof proc.off !== "function") return
    for (const [sig, handler] of this.signalHandlers) {
      try {
        proc.off(sig, handler)
      } catch {
        // ignore
      }
    }
    this.signalHandlers = []
  }
}

function randomId(len: number): string {
  const g = globalThis as { crypto?: { randomUUID?: () => string } }
  if (g.crypto?.randomUUID !== undefined) {
    return g.crypto.randomUUID().replace(/-/g, "").slice(0, len)
  }
  return `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`.slice(0, len)
}
