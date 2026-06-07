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

export async function getServerSession() {
  return auth.api.getSession({ headers: await headers() })
}

export async function requireCustomer() {
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
