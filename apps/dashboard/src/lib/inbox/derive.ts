// SPDX-License-Identifier: Apache-2.0

import type {
  Approval,
  HomeOverview,
} from "@/lib/api"

import type {
  InboxFilters,
  InboxItem,
  InboxKind,
  InboxSeverity,
} from "./types"

// Approval kind doesn't carry a severity field today. v1 mapping: every
// HITL gate is "warning" (someone needs to act, but the system itself is
// fine). System alerts emit their own severity from the runtime.
function severityForApproval(_kind: string): InboxSeverity {
  return "warning"
}

function approvalContext(a: Approval): string {
  const parts: string[] = []
  if (a.kind) parts.push(a.kind)
  if (a.requested_by) parts.push(`requested by ${a.requested_by}`)
  if (a.tenant_id) parts.push(`tenant ${a.tenant_id.slice(0, 8)}`)
  return parts.join(" · ")
}

function approvalTitle(a: Approval): string {
  // Try common payload keys first so the row is readable. Fallback to the
  // approval kind which is always present.
  const payload = a.payload as Record<string, unknown>
  for (const key of ["title", "summary", "subject", "description"]) {
    const v = payload?.[key]
    if (typeof v === "string" && v.trim()) return v
  }
  return `Approval: ${a.kind}`
}

function alertSeverity(raw: string): InboxSeverity {
  if (raw === "critical") return "critical"
  if (raw === "warning") return "warning"
  return "info"
}

// Merge the two v1 sources into a single typed list. Order: severity tier
// (critical → warning → info), newest first within tier. The page's filter
// chips are applied after this so the count we show matches the rendered
// list.
export function mergeInboxItems(
  approvals: Approval[],
  alerts: HomeOverview["alerts"],
  fetchedAt: string,
): InboxItem[] {
  const items: InboxItem[] = []

  for (const a of approvals) {
    items.push({
      kind: "approval",
      id: `approval:${a.id}`,
      sourceId: a.id,
      severity: severityForApproval(a.kind),
      title: approvalTitle(a),
      context: approvalContext(a),
      occurredAt: a.created_at,
      approval: a,
    })
  }

  for (const alert of alerts ?? []) {
    items.push({
      kind: "alert",
      id: `alert:${alert.id}`,
      sourceId: alert.id,
      severity: alertSeverity(alert.severity),
      title: alert.title,
      context: alert.description ?? "",
      // Alerts don't carry an occurred_at — they're recomputed every poll.
      // Use the snapshot fetched_at so they fall into "now" age bucket.
      occurredAt: fetchedAt,
    })
  }

  return sortInboxItems(items)
}

const SEVERITY_TIER: Record<InboxSeverity, number> = {
  critical: 0,
  warning: 1,
  info: 2,
}

export function sortInboxItems(items: InboxItem[]): InboxItem[] {
  return [...items].sort((a, b) => {
    const tierDiff = SEVERITY_TIER[a.severity] - SEVERITY_TIER[b.severity]
    if (tierDiff !== 0) return tierDiff
    return a.occurredAt < b.occurredAt ? 1 : -1
  })
}

export function applyFilters(
  items: InboxItem[],
  filters: InboxFilters,
): InboxItem[] {
  return items.filter((item) => {
    if (filters.severity !== "all" && item.severity !== filters.severity) {
      return false
    }
    if (filters.kind !== "all" && item.kind !== filters.kind) {
      return false
    }
    return true
  })
}

export function groupBySeverity(
  items: InboxItem[],
): { severity: InboxSeverity; items: InboxItem[] }[] {
  const groups: Record<InboxSeverity, InboxItem[]> = {
    critical: [],
    warning: [],
    info: [],
  }
  for (const item of items) groups[item.severity].push(item)
  return (["critical", "warning", "info"] as InboxSeverity[])
    .filter((s) => groups[s].length > 0)
    .map((severity) => ({ severity, items: groups[severity] }))
}

export function countsByKind(items: InboxItem[]): Record<InboxKind, number> {
  return items.reduce<Record<InboxKind, number>>(
    (acc, item) => {
      acc[item.kind] += 1
      return acc
    },
    { approval: 0, alert: 0 },
  )
}

export function countsBySeverity(
  items: InboxItem[],
): Record<InboxSeverity, number> {
  return items.reduce<Record<InboxSeverity, number>>(
    (acc, item) => {
      acc[item.severity] += 1
      return acc
    },
    { critical: 0, warning: 0, info: 0 },
  )
}
