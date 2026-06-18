// SPDX-License-Identifier: Apache-2.0

import type {
  AdminService,
  DBHealth,
  MetricsSummary,
  ProviderHealth,
} from "@/lib/api"

// Server-rendered snapshot. Every source degrades independently — a
// half-loaded page is better than a blank one; the client polling tick
// recovers the missing piece on the next round.

export interface HealthSnapshot {
  services: AdminService[]
  providers: ProviderHealth[]
  providerWindow: ProviderWindowKind
  db: DBHealth | null
  metrics: MetricsSummary | null
  fetchedAt: string
  servicesHealthy: boolean
  providersHealthy: boolean
  dbHealthy: boolean
  metricsHealthy: boolean
}

export type ProviderWindowKind = "24h" | "7d" | "30d"

export function isProviderWindowKind(v: string): v is ProviderWindowKind {
  return v === "24h" || v === "7d" || v === "30d"
}

// "healthy" — every service + provider in scope is healthy
// "degraded" — at least one is degraded but no critical signal is down
// "down"     — at least one critical surface is offline
// "unknown"  — we don't have enough data to decide
export type OverallStatus = "healthy" | "degraded" | "down" | "unknown"

export interface OverallSummary {
  status: OverallStatus
  servicesHealthy: number
  servicesDegraded: number
  servicesDown: number
  providersHealthy: number
  providersDegraded: number
  providersDown: number
}
