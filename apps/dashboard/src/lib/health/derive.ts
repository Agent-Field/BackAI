// SPDX-License-Identifier: Apache-2.0

import type {
  AdminService,
  ProviderHealth,
} from "@/lib/api"
import type { StatusState } from "@/lib/home/types"

import type { HealthSnapshot, OverallStatus, OverallSummary } from "./types"

// Service status helpers ---------------------------------------------------

export function classifyServiceStatus(raw: string): StatusState {
  switch (raw) {
    case "healthy":
      return "ok"
    case "degraded":
      return "watch"
    case "offline":
    case "down":
      return "act"
    default:
      return "idle"
  }
}

export function classifyProviderStatus(raw: string): StatusState {
  switch (raw) {
    case "healthy":
      return "ok"
    case "degraded":
      return "watch"
    case "down":
      return "act"
    default:
      return "idle"
  }
}

// Overall summary ----------------------------------------------------------

export function summarise(snapshot: HealthSnapshot): OverallSummary {
  const s = countByStatus(snapshot.services)
  const p = countByStatus(snapshot.providers, "provider")
  const status: OverallStatus = (() => {
    if (s.down > 0) return "down"
    if (s.degraded > 0 || p.degraded > 0 || p.down > 0) return "degraded"
    if (snapshot.services.length === 0 && snapshot.providers.length === 0) {
      return "unknown"
    }
    return "healthy"
  })()
  return {
    status,
    servicesHealthy: s.healthy,
    servicesDegraded: s.degraded,
    servicesDown: s.down,
    providersHealthy: p.healthy,
    providersDegraded: p.degraded,
    providersDown: p.down,
  }
}

function countByStatus(
  rows: AdminService[] | ProviderHealth[],
  kind: "service" | "provider" = "service",
): { healthy: number; degraded: number; down: number } {
  let healthy = 0
  let degraded = 0
  let down = 0
  for (const r of rows) {
    const raw = (r as { status: string }).status
    if (raw === "healthy") healthy++
    else if (raw === "degraded") degraded++
    else if (raw === "offline" || raw === "down") down++
    else if (kind === "provider" && raw === "unknown") {
      /* don't count toward any bucket */
    } else if (raw === "configured") {
      /* idle services pass through */
    }
  }
  return { healthy, degraded, down }
}

// Service grouping ---------------------------------------------------------

const KIND_GROUP_ORDER = [
  "runtime",
  "data",
  "llm-gateway",
  "agent-runtime",
  "storage",
  "queue",
  "delivery",
  "observability",
  "other",
] as const

export type ServiceGroupKey = (typeof KIND_GROUP_ORDER)[number]

const KIND_GROUP_LABELS: Record<ServiceGroupKey, string> = {
  runtime: "Runtime",
  data: "Data",
  "llm-gateway": "Intelligence",
  "agent-runtime": "Intelligence",
  storage: "Storage",
  queue: "Queue",
  delivery: "Delivery",
  observability: "Observability",
  other: "Other",
}

export function groupServicesByKind(
  services: AdminService[],
): { key: ServiceGroupKey; label: string; services: AdminService[] }[] {
  const buckets = new Map<ServiceGroupKey, AdminService[]>()
  for (const s of services) {
    const key = mapKindToGroup(s.kind)
    if (!buckets.has(key)) buckets.set(key, [])
    buckets.get(key)!.push(s)
  }
  return KIND_GROUP_ORDER.filter((k) => buckets.has(k)).map((k) => ({
    key: k,
    // Two kinds share the "Intelligence" label so dedupe by label.
    label: KIND_GROUP_LABELS[k],
    services: buckets.get(k)!,
  }))
}

function mapKindToGroup(kind: string): ServiceGroupKey {
  if (kind.startsWith("observability")) return "observability"
  if (kind === "runtime") return "runtime"
  if (kind === "data" || kind === "database") return "data"
  if (kind === "llm-gateway" || kind === "llm") return "llm-gateway"
  if (kind === "agent-runtime" || kind === "agentfield") return "agent-runtime"
  if (kind === "storage" || kind === "object-storage") return "storage"
  if (kind === "queue" || kind === "jobs") return "queue"
  if (kind === "delivery" || kind === "webhooks") return "delivery"
  return "other"
}

// Provider sparkline helper ------------------------------------------------

// latency_buckets arrives as a `Record<string, number>` keyed by RFC3339
// timestamps. We sort by key (chronological) and emit values for the
// Sparkline primitive.
export function providerSparkline(p: ProviderHealth): number[] {
  const entries = Object.entries(p.latency_buckets)
  if (entries.length === 0) return []
  entries.sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
  return entries.map(([, v]) => v)
}

// Formatting helpers -------------------------------------------------------

export function formatLatency(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "—"
  if (ms < 1) return "<1ms"
  if (ms < 1000) return `${Math.round(ms)}ms`
  return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)}s`
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "—"
  const KB = 1024
  const MB = KB * 1024
  const GB = MB * 1024
  if (bytes < KB) return `${bytes} B`
  if (bytes < MB) return `${Math.round(bytes / KB)} KB`
  if (bytes < GB) return `${(bytes / MB).toFixed(1)} MB`
  return `${(bytes / GB).toFixed(2)} GB`
}

export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return "—"
  const d = Math.floor(seconds / 86_400)
  const h = Math.floor((seconds % 86_400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h ${m}m`
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m`
  return `${Math.round(seconds)}s`
}

export function formatRelative(iso: string | null): string {
  if (!iso) return "—"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffSec = Math.max(0, Math.round((Date.now() - ts) / 1000))
  if (diffSec < 5) return "now"
  if (diffSec < 60) return `${diffSec}s ago`
  if (diffSec < 3600) return `${Math.round(diffSec / 60)}m ago`
  if (diffSec < 86_400) return `${Math.round(diffSec / 3600)}h ago`
  return new Date(ts).toISOString().slice(5, 10)
}
