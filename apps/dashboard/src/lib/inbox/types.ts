// SPDX-License-Identifier: Apache-2.0

import type { Approval } from "@/lib/api"

// Inbox is a typed union of two v1 item sources. Approvals come from
// suite_approvals (HITL gates). System alerts come from the dependency
// probes folded into /api/v1/home/overview. The Inbox page merges and
// orders them client-side (Gap 8 — backend merge endpoint deferred).

export type InboxSeverity = "critical" | "warning" | "info"

export type InboxKind = "approval" | "alert"

// Discriminated union: every item has a stable composite id so the URL
// drawer (`?item=approval:<id>`) and the filter state can address one
// item regardless of source.
export type InboxItem =
  | {
      kind: "approval"
      id: string // composite: `approval:<uuid>`
      sourceId: string // raw approval id
      severity: InboxSeverity
      title: string
      context: string
      occurredAt: string
      approval: Approval
    }
  | {
      kind: "alert"
      id: string // composite: `alert:<slug>`
      sourceId: string
      severity: InboxSeverity
      title: string
      context: string
      occurredAt: string
    }

// What the server renders into the page on first paint. Failures degrade
// silently — the page still mounts and surfaces a banner per data-state.
export interface InboxSnapshot {
  items: InboxItem[]
  approvalsHealthy: boolean
  alertsHealthy: boolean
  fetchedAt: string
}

export interface InboxFilters {
  severity: InboxSeverity | "all"
  kind: InboxKind | "all"
}

export const DEFAULT_FILTERS: InboxFilters = {
  severity: "all",
  kind: "all",
}
