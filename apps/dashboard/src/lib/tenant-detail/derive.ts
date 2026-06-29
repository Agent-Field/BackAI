// SPDX-License-Identifier: Apache-2.0

import type { APIKey, TenantDrilldown } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"

import type { AtRiskSignal } from "./types"

// At-risk composite (Gap 33 mitigation) ---------------------------------

// Compute per-signal at-risk reasons from the drilldown payload. The
// header surfaces one AtRiskAlert per signal; if any is "critical" the
// status badge flips to destructive.
export function deriveAtRiskSignals(
  drilldown: TenantDrilldown,
): AtRiskSignal[] {
  const signals: AtRiskSignal[] = []

  if (drilldown.tenant.deleted_at) {
    signals.push({
      id: "tenant-deleted",
      severity: "critical",
      message: "Tenant marked deleted",
      detail: "All API access is rejected until restored.",
    })
  }

  // Recent failure rate: scan recent_runs for non-2xx outcomes.
  if (drilldown.recent_runs.length > 0) {
    const failures = drilldown.recent_runs.filter(
      (r) => r.status === "failed",
    ).length
    const rate = failures / drilldown.recent_runs.length
    if (rate >= 0.5) {
      signals.push({
        id: "recent-runs-failing",
        severity: "critical",
        message: `${Math.round(rate * 100)}% of recent runs failing`,
        detail: `${failures} of ${drilldown.recent_runs.length} recent runs`,
      })
    } else if (rate >= 0.15) {
      signals.push({
        id: "recent-runs-elevated-errors",
        severity: "warning",
        message: `${Math.round(rate * 100)}% of recent runs failing`,
        detail: `${failures} of ${drilldown.recent_runs.length} recent runs`,
      })
    }
  }

  // Key approaching its budget cap.
  for (const key of drilldown.api_keys) {
    if (!key.budget_max_usd || key.budget_max_usd <= 0) continue
    const spent = key.live_spend_usd ?? 0
    const pct = spent / key.budget_max_usd
    if (pct >= 1) {
      signals.push({
        id: `key-over:${key.id}`,
        severity: "critical",
        message: `Key "${key.name ?? key.prefix}" is over budget`,
        detail: `${formatCurrency(spent)} of ${formatCurrency(key.budget_max_usd)}`,
      })
    } else if (pct >= 0.8) {
      signals.push({
        id: `key-near:${key.id}`,
        severity: "warning",
        message: `Key "${key.name ?? key.prefix}" near budget`,
        detail: `${Math.round(pct * 100)}% of ${formatCurrency(key.budget_max_usd)}`,
      })
    }
  }

  // Trial expiring within 7 days.
  if (drilldown.billing?.trial_ends_at) {
    const ts = Date.parse(drilldown.billing.trial_ends_at)
    if (!Number.isNaN(ts)) {
      const daysLeft = (ts - Date.now()) / 86_400_000
      if (daysLeft >= 0 && daysLeft <= 7) {
        signals.push({
          id: "trial-expiring",
          severity: "warning",
          message: `Trial ends in ${Math.max(1, Math.round(daysLeft))}d`,
          detail: "Convert before traffic gets cut off.",
        })
      } else if (daysLeft < 0) {
        signals.push({
          id: "trial-expired",
          severity: "critical",
          message: "Trial has expired",
        })
      }
    }
  }

  return signals
}

export function tenantStatusState(
  drilldown: TenantDrilldown,
  signals: AtRiskSignal[],
): StatusState {
  if (drilldown.tenant.deleted_at) return "act"
  if (signals.some((s) => s.severity === "critical")) return "act"
  if (signals.length > 0) return "watch"
  return "ok"
}

export function tenantStatusLabel(
  drilldown: TenantDrilldown,
  state: StatusState,
): string {
  if (drilldown.tenant.deleted_at) return "deleted"
  switch (state) {
    case "ok":
      return "active"
    case "watch":
      return "at risk"
    case "act":
      return "critical"
    default:
      return "unknown"
  }
}

// Key helpers ------------------------------------------------------------

export function classifyKeyStatus(key: APIKey): "active" | "revoked" | "expired" {
  if (key.revoked_at) return "revoked"
  if (key.expires_at) {
    const ts = Date.parse(key.expires_at)
    if (!Number.isNaN(ts) && ts < Date.now()) return "expired"
  }
  return "active"
}

export function maskKey(prefix: string): string {
  return `${prefix}••••••••••••`
}

// Activity feed merge (Gap 35 mitigation) --------------------------------

export interface TenantActivityEvent {
  id: string
  kind: "run" | "webhook"
  title: string
  context: string
  status: string
  severity: "ok" | "watch" | "act" | "idle"
  occurredAt: string
}

export function mergeActivity(
  drilldown: TenantDrilldown,
): TenantActivityEvent[] {
  const events: TenantActivityEvent[] = []
  for (const run of drilldown.recent_runs) {
    events.push({
      id: `run:${run.id}`,
      kind: "run",
      title: run.agent,
      context: `${formatRunDuration(run.duration_ms)} · ${formatCurrency(run.cost_usd)}`,
      status: run.status,
      severity:
        run.status === "failed" ? "act" : run.status === "succeeded" ? "ok" : "watch",
      occurredAt: run.started_at,
    })
  }
  for (const wh of drilldown.recent_webhooks) {
    events.push({
      id: `webhook:${wh.id}`,
      kind: "webhook",
      title: `${wh.direction}: ${wh.event_type}`,
      context: wh.status,
      status: wh.status,
      severity:
        wh.status === "failed" ? "act" : wh.status === "queued" ? "watch" : "ok",
      occurredAt: wh.created_at,
    })
  }
  return events.sort((a, b) => (a.occurredAt < b.occurredAt ? 1 : -1))
}

// Formatters -------------------------------------------------------------

export function formatCurrency(n?: number | null): string {
  if (n === undefined || n === null) return "—"
  if (n === 0) return "$0"
  if (Math.abs(n) < 1) return `$${n.toFixed(4)}`
  if (Math.abs(n) < 100) return `$${n.toFixed(2)}`
  return `$${Math.round(n).toLocaleString()}`
}

export function formatNumber(n?: number | null): string {
  if (n === undefined || n === null) return "—"
  if (n === 0) return "0"
  if (n < 1000) return n.toString()
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 2 : 1)}k`
  return `${(n / 1_000_000).toFixed(1)}M`
}

export function formatBytes(bytes?: number | null): string {
  if (!bytes || bytes <= 0) return "—"
  const KB = 1024
  const MB = KB * 1024
  const GB = MB * 1024
  if (bytes < KB) return `${bytes} B`
  if (bytes < MB) return `${Math.round(bytes / KB)} KB`
  if (bytes < GB) return `${(bytes / MB).toFixed(1)} MB`
  return `${(bytes / GB).toFixed(2)} GB`
}

export function formatRunDuration(ms: number): string {
  if (!ms || ms <= 0) return "—"
  if (ms < 1000) return `${Math.round(ms)}ms`
  return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)}s`
}

export function formatAge(iso: string | null): string {
  if (!iso) return "—"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = Math.max(0, Date.now() - ts)
  if (diffMs < 1000) return "now"
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86_400) return `${Math.floor(sec / 3600)}h ago`
  return new Date(ts).toISOString().slice(5, 10)
}
