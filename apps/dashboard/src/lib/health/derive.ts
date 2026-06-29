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

// Group order is the rendering order for Zone B. Critical infra first
// (Runtime / Data), then Intelligence (LLM gateway + agent runtime),
// then storage / queue / delivery, then observability, then anything
// uncategorised. AgentField belongs in INTELLIGENCE per brief A3.
const KIND_GROUP_ORDER = [
  "runtime",
  "data",
  "intelligence",
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
  intelligence: "Intelligence",
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
    label: KIND_GROUP_LABELS[k],
    services: buckets.get(k)!,
  }))
}

// Backend service `kind` strings come from `kindBySlot` in
// services.go: "llm-gateway", "reasoning", "storage", "webhooks",
// "notifications", "billing", "queue". Mapping below collapses them
// into the brief's eight-group taxonomy.
function mapKindToGroup(kind: string): ServiceGroupKey {
  if (kind.startsWith("observability")) return "observability"
  if (kind === "runtime") return "runtime"
  if (kind === "data" || kind === "database") return "data"
  if (
    kind === "llm-gateway" ||
    kind === "llm" ||
    kind === "reasoning" ||
    kind === "agent-runtime" ||
    kind === "agentfield"
  ) {
    return "intelligence"
  }
  if (kind === "storage" || kind === "object-storage") return "storage"
  if (kind === "queue" || kind === "jobs" || kind === "job-queue") return "queue"
  if (
    kind === "delivery" ||
    kind === "webhooks" ||
    kind === "notifications" ||
    kind === "billing"
  ) {
    return "delivery"
  }
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

// Threshold-based status override. The brief A2 mandates that the
// rendered status reflect BOTH uptime AND p95 latency — a green pill
// next to red latency is the worst possible visual contradiction.
//
//   healthy  → uptime ≥ 99% AND p95 ≤ 1s
//   degraded → uptime ≥ 90% OR  p95 ≤ 5s   (one signal slipping)
//   down     → both signals failed
//
// We layer this over whatever the backend says so the dashboard never
// disagrees with itself, even when the runtime hasn't shipped the same
// threshold table yet.
export function deriveProviderStatus(p: ProviderHealth): "healthy" | "degraded" | "down" | "unknown" {
  if (p.observations === 0) return "unknown"
  const uptime = p.availability_pct / 100
  const p95 = p.p95_latency_ms
  if (uptime >= 0.99 && p95 > 0 && p95 <= 1000) return "healthy"
  if (uptime >= 0.9 || (p95 > 0 && p95 <= 5000)) return "degraded"
  return "down"
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

// Single timestamp convention for the Health page (C5). "now" is
// reserved for genuinely sub-second age; everything else uses
// "Ns / Nm / Nh ago" so providers and services agree on tense.
export function formatRelative(iso: string | null): string {
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
