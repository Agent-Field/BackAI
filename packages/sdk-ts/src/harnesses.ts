// SPDX-License-Identifier: Apache-2.0

// suite.harnesses.* — Phase 11.4 CLI harness probes.
//
// Endpoint paths and JSON shapes are the canonical contract from
// `apps/dashboard/src/lib/api.ts` (the Phase 11.4 Harnesses section).
// Schemas below mirror the zod schemas there; if the dashboard contract
// moves, move these together.
//
// The SDK is read-only: list / get / probe. There is no "install" verb
// — operators install the CLI themselves (npm / pip / brew / curl) and
// then call `probe()` to re-detect.

import { z } from "zod"
import { request, type HttpOptions } from "./_http.js"

// ---------- shared schemas (mirror lib/api.ts) ----------

export const HarnessProviderSchema = z.enum([
  "claude-code",
  "codex",
  "gemini",
  "opencode",
])
export type HarnessProvider = z.infer<typeof HarnessProviderSchema>

export const HarnessStatusSchema = z.enum([
  "ready",
  "needs_auth",
  "missing",
  "errored",
])
export type HarnessStatus = z.infer<typeof HarnessStatusSchema>

export const HarnessSchema = z.object({
  provider: HarnessProviderSchema,
  isInstalled: z.boolean(),
  binaryPath: z.string().nullable(),
  version: z.string().nullable(),
  status: HarnessStatusSchema,
  lastError: z.string().nullable(),
  requiredEnv: z.array(z.string()),
  installDocUrl: z.string().nullable(),
})
export type Harness = z.infer<typeof HarnessSchema>

export const HarnessListSchema = z.object({
  harnesses: z.array(HarnessSchema),
})
export type HarnessList = z.infer<typeof HarnessListSchema>

const ALLOWED_PROVIDERS: readonly HarnessProvider[] = [
  "claude-code",
  "codex",
  "gemini",
  "opencode",
]

function assertProvider(provider: string): asserts provider is HarnessProvider {
  if (!(ALLOWED_PROVIDERS as readonly string[]).includes(provider)) {
    throw new Error(
      `provider must be one of: ${ALLOWED_PROVIDERS.join(", ")}; got ${JSON.stringify(provider)}`,
    )
  }
}

// ---------- public API ----------

/** List every supported harness with its current probe status. */
export async function list(opts: HttpOptions = {}): Promise<HarnessList> {
  const raw = await request<unknown>("GET", "/harnesses", null, opts)
  return HarnessListSchema.parse(camelizeList(raw))
}

/** Fetch the cached probe result for a single provider. */
export async function get(
  provider: HarnessProvider | string,
  opts: HttpOptions = {},
): Promise<Harness> {
  assertProvider(provider)
  const raw = await request<unknown>(
    "GET",
    `/harnesses/${encodeURIComponent(provider)}`,
    null,
    opts,
  )
  return HarnessSchema.parse(camelizeHarness(raw))
}

/** Force a fresh probe of the named harness and return the result. */
export async function probe(
  provider: HarnessProvider | string,
  opts: HttpOptions = {},
): Promise<Harness> {
  assertProvider(provider)
  const raw = await request<unknown>(
    "POST",
    `/harnesses/${encodeURIComponent(provider)}/probe`,
    null,
    opts,
  )
  return HarnessSchema.parse(camelizeHarness(raw))
}

// ---------- helpers ----------

/**
 * Translate the snake_case wire envelope for a single harness into the
 * camelCase shape declared by `HarnessSchema`.
 */
function camelizeHarness(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  return {
    provider: src.provider,
    isInstalled: src.is_installed ?? src.isInstalled ?? false,
    binaryPath: src.binary_path ?? src.binaryPath ?? null,
    version: src.version ?? null,
    status: src.status,
    lastError: src.last_error ?? src.lastError ?? null,
    requiredEnv: src.required_env ?? src.requiredEnv ?? [],
    installDocUrl: src.install_doc_url ?? src.installDocUrl ?? null,
  }
}

function camelizeList(raw: unknown): unknown {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return raw
  const src = raw as Record<string, unknown>
  const harnesses = Array.isArray(src.harnesses)
    ? src.harnesses.map(camelizeHarness)
    : src.harnesses
  return { harnesses }
}

/** Namespace object — the shape `suite.harnesses` is built from. */
export const harnesses = {
  list,
  get,
  probe,
} as const