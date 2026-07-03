// SPDX-License-Identifier: Apache-2.0

// Server-only session helpers for the customer-app.

import { headers } from "next/headers"
import { redirect } from "next/navigation"

import { auth } from "@/lib/auth"
import {
  lookupCustomerContext,
  provisionTenant,
  type ProvisionResult,
} from "@/lib/provisioning"

/**
 * Personal mode (AF_STACK_MODE=personal): single-user app, no customer
 * sign-in. Read server-side only so it stays a true runtime toggle.
 */
export function isPersonalMode(): boolean {
  return process.env.AF_STACK_MODE === "personal"
}

// Synthetic customer session used in personal mode, where there is no
// sign-in and therefore no real better-auth session. Shaped like a real
// session so server components (which read user.name / user.email) render.
function personalCustomerSession() {
  const now = new Date()
  return {
    session: {
      id: "personal",
      token: "personal",
      userId: "personal",
      expiresAt: new Date(now.getTime() + 24 * 60 * 60 * 1000),
      createdAt: now,
      updatedAt: now,
    },
    user: {
      id: "personal",
      email: "you@localhost",
      name: "Personal",
      emailVerified: true,
      createdAt: now,
      updatedAt: now,
    },
  }
}

// Synthetic tenant context for personal mode. The runtime serves everything
// off the default tenant, so we never run per-user provisioning here.
function personalCustomerContext() {
  return {
    tenantId: "default",
    tenantName: "Personal",
    tenantSlug: "default",
    suiteUserId: "personal",
    apiKeyPrefix: null,
  }
}

export async function getServerSession() {
  if (isPersonalMode()) {
    return personalCustomerSession() as Awaited<
      ReturnType<typeof auth.api.getSession>
    >
  }
  return auth.api.getSession({ headers: await headers() })
}

export async function requireCustomer() {
  // Personal mode has no sign-in gate — return a synthetic customer so
  // customer-gated server components render without a real session.
  if (isPersonalMode()) {
    return personalCustomerSession() as NonNullable<
      Awaited<ReturnType<typeof auth.api.getSession>>
    >
  }
  const session = await getServerSession()
  if (!session) {
    redirect("/sign-in")
  }
  return session
}

/**
 * Returns the customer's tenant context, provisioning lazily if missing.
 * This handles the case where the auth-hook provisioning swallowed an
 * error (or hasn't run yet for some other reason).
 */
export async function requireCustomerContext() {
  const session = await requireCustomer()
  // Personal mode uses the runtime's default tenant — never provision a
  // per-user tenant chain here.
  if (isPersonalMode()) {
    return { session, ctx: personalCustomerContext() }
  }
  let ctx = await lookupCustomerContext(session.user.email)
  if (!ctx) {
    // Lazy fallback: this should normally have happened in the auth
    // hook. We never surface the api-key token through this path —
    // returning customers see only the prefix on the dashboard.
    const provisioned: ProvisionResult = await provisionTenant({
      email: session.user.email,
      name: session.user.name,
      withApiKey: false,
    })
    ctx = {
      tenantId: provisioned.tenantId,
      tenantName: provisioned.tenantName,
      tenantSlug: provisioned.tenantSlug,
      suiteUserId: provisioned.suiteUserId,
      apiKeyPrefix: provisioned.apiKeyPrefix,
    }
  }
  return { session, ctx }
}
