// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"
import type { BillingPlan, BillingSettingsStatus } from "@/lib/api"

// Server-side fetch for Platform → Billing. Tolerant of failure: the page
// still mounts (with an explanatory empty state) if the runtime is
// unreachable or the operator lacks the admin billing scope.

export interface BillingSnapshot {
  settings: BillingSettingsStatus | null
  plans: BillingPlan[]
  fetchedAt: string
  ok: boolean
}

export async function fetchBillingSnapshot(): Promise<BillingSnapshot> {
  const fetchedAt = new Date().toISOString()
  const [settings, plans] = await Promise.all([
    api.admin.billing.settings().catch(() => null),
    api.billing.plans().catch(() => null),
  ])
  return {
    settings,
    plans: plans?.plans ?? [],
    fetchedAt,
    ok: settings !== null,
  }
}
