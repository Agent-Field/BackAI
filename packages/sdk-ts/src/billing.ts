// SPDX-License-Identifier: Apache-2.0

// suite.billing.* — Stripe billing + per-tenant usage meters.
//
// Endpoint paths and JSON shapes are the canonical contract from
// `apps/dashboard/src/lib/api.ts` (the Phase 10.4 Billing section). The
// zod schemas below mirror the dashboard schemas exactly; if the
// dashboard contract moves, move these together.
//
// Public API stays camelCase; the wire stays snake_case and is
// translated via `toCamel` from `_http.ts`.

import { z } from "zod"
import { request, toCamel, type HttpOptions } from "./_http.js"

// ---------- shared schemas (mirror lib/api.ts, camelCase) ----------

export const BillingCustomerSchema = z.object({
  tenantId: z.string(),
  stripeCustomerId: z.string().nullable(),
  email: z.string().nullable(),
  plan: z.string(),
  trialEndsAt: z.string().nullable(),
  currentPeriodEnd: z.string().nullable(),
  subscriptionStatus: z.string().nullable(),
  createdAt: z.string(),
  updatedAt: z.string(),
})
export type BillingCustomer = z.infer<typeof BillingCustomerSchema>

export const BillingCustomerListSchema = z.object({
  customers: z.array(BillingCustomerSchema),
})
export type BillingCustomerList = z.infer<typeof BillingCustomerListSchema>

export const UsageMeterSchema = z.object({
  meter: z.string(),
  tenantId: z.string(),
  periodStart: z.string(),
  periodEnd: z.string(),
  quantity: z.number(),
  costUsd: z.number().nullable(),
  stripeMeterId: z.string().nullable(),
  lastSyncedAt: z.string().nullable(),
})
export type UsageMeter = z.infer<typeof UsageMeterSchema>

export const UsageMeterListSchema = z.object({
  meters: z.array(UsageMeterSchema),
  totalCostUsd: z.number(),
})
export type UsageMeterList = z.infer<typeof UsageMeterListSchema>

export const PortalLinkSchema = z.object({
  url: z.string(),
  expiresAt: z.string(),
})
export type PortalLink = z.infer<typeof PortalLinkSchema>

export type Bucket = "month" | "day"

// ---------- public API ----------

export interface ListMetersOptions extends HttpOptions {
  tenant?: string
  /** ISO 8601 timestamp; defaults to current month start. */
  periodStart?: string
  bucket?: Bucket
}

/** List every billing customer. */
export async function customers(
  opts: HttpOptions = {},
): Promise<BillingCustomerList> {
  const raw = await request<unknown>("GET", "/billing/customers", null, opts)
  return BillingCustomerListSchema.parse(toCamel(raw))
}

/** Fetch a tenant's billing customer. */
export async function customer(
  tenantId: string,
  opts: HttpOptions = {},
): Promise<BillingCustomer> {
  if (typeof tenantId !== "string" || tenantId.length === 0) {
    throw new Error("tenantId must be a non-empty string")
  }
  const raw = await request<unknown>(
    "GET",
    `/billing/customers/${encodeURIComponent(tenantId)}`,
    null,
    opts,
  )
  return BillingCustomerSchema.parse(toCamel(raw))
}

/** List usage meters for the given filters. */
export async function meters(
  opts: ListMetersOptions = {},
): Promise<UsageMeterList> {
  const { tenant, periodStart, bucket = "month", ...http } = opts
  const qs = new URLSearchParams()
  if (tenant !== undefined && tenant !== "") qs.set("tenant", tenant)
  if (periodStart !== undefined && periodStart !== "")
    qs.set("period_start", periodStart)
  if (bucket !== undefined) qs.set("bucket", bucket)
  const q = qs.toString()
  const raw = await request<unknown>(
    "GET",
    q ? `/billing/meters?${q}` : "/billing/meters",
    null,
    http,
  )
  return UsageMeterListSchema.parse(toCamel(raw))
}

export interface PortalLinkOptions extends HttpOptions {
  /** URL the customer is sent back to after closing the portal. */
  returnUrl?: string
}

/** Mint a short-lived Stripe Customer Portal link for the tenant.
 *
 * In stub mode (no `STRIPE_SECRET_KEY` on the runtime), the URL points
 * at `example.com` so the dashboard can still render the button.
 */
export async function portalLink(
  tenantId: string,
  opts: PortalLinkOptions = {},
): Promise<PortalLink> {
  if (typeof tenantId !== "string" || tenantId.length === 0) {
    throw new Error("tenantId must be a non-empty string")
  }
  const { returnUrl, ...http } = opts
  const body: Record<string, unknown> = {}
  if (returnUrl !== undefined && returnUrl !== "") body.return_url = returnUrl
  const raw = await request<unknown>(
    "POST",
    `/billing/customers/${encodeURIComponent(tenantId)}/portal`,
    body,
    http,
  )
  return PortalLinkSchema.parse(toCamel(raw))
}

// ---------- in-process verbs (parity with the Python SDK) ----------

export interface MeterOptions extends HttpOptions {
  tenantId?: string
}

/** Record `qty` units of `name` for the tenant's current period.
 *
 * The runtime owns the cost computation — the SDK forwards the increment to
 * `POST /api/v1/billing/meter`. When `tenantId` is omitted the runtime uses
 * the authenticated caller's tenant (the normal app-code path).
 */
export async function meter(
  name: string,
  qty: number,
  opts: MeterOptions = {},
): Promise<void> {
  if (typeof name !== "string" || name.length === 0) {
    throw new Error("meter name must be a non-empty string")
  }
  if (qty < 0) {
    throw new Error("qty must be non-negative")
  }
  const { tenantId, ...http } = opts
  const body: Record<string, unknown> = { name, qty }
  if (tenantId !== undefined && tenantId !== "") body.tenant_id = tenantId
  await request<void>("POST", "/billing/meter", body, http)
}

const PLAN_CAPS: Record<string, number> = {
  free: 10.0,
  pro: 500.0,
  enterprise: 0.0,
}

/** Check whether the tenant has room for an additional spend.
 *
 * Composes the gate from the existing read endpoints. Future
 * Phase 10.5 work may expose a dedicated /api/v1/billing/has-budget
 * endpoint that short-circuits this round-trip.
 */
export async function hasBudget(
  tenantId: string,
  additionalUsd: number,
  opts: HttpOptions = {},
): Promise<boolean> {
  if (!tenantId) return true
  if (additionalUsd < 0) {
    throw new Error("additionalUsd must be non-negative")
  }
  let plan = "free"
  try {
    const cust = await customer(tenantId, opts)
    plan = cust.plan
  } catch {
    return true
  }
  const cap = PLAN_CAPS[plan] ?? 0
  if (cap <= 0) {
    // Unknown plan / enterprise == unlimited.
    return true
  }
  const usage = await meters({ tenant: tenantId, ...opts })
  return usage.totalCostUsd + additionalUsd <= cap
}

/** Namespace object — the shape `suite.billing` is built from. */
export const billing = {
  customers,
  customer,
  meters,
  portalLink,
  meter,
  hasBudget,
} as const