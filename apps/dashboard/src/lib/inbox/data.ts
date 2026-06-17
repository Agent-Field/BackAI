// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import { mergeInboxItems } from "./derive"
import type { InboxSnapshot } from "./types"

// Server-side initial fetch. Both sources tolerate failure: a half-degraded
// page is better than a blank one, and the client-side polling tick will
// recover the missing half on the next round.
export async function fetchInboxSnapshot(): Promise<InboxSnapshot> {
  const fetchedAt = new Date().toISOString()
  const [approvalsResult, homeResult] = await Promise.allSettled([
    api.approvals.list({ status: "pending", limit: 100 }),
    api.home(),
  ])

  const approvals =
    approvalsResult.status === "fulfilled"
      ? approvalsResult.value.approvals
      : []
  const alerts =
    homeResult.status === "fulfilled" ? homeResult.value.alerts : []

  return {
    items: mergeInboxItems(approvals, alerts, fetchedAt),
    approvalsHealthy: approvalsResult.status === "fulfilled",
    alertsHealthy: homeResult.status === "fulfilled",
    fetchedAt,
  }
}
