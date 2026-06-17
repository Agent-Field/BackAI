// SPDX-License-Identifier: Apache-2.0

import type { Budget, CacheStats, CostSummary, Tenant } from "@/lib/api"

// Server-rendered snapshot for the Cost page. Failures degrade
// independently — a half-loaded page is better than a blank one, and the
// client polling tick recovers missing pieces on the next round.

export interface CostSnapshot {
  primary: CostSummary | null
  // Per-tenant secondary calls so Zone 3 can stack series and Zone 2 can
  // expand a tenant into its agents. Keyed by tenant_id.
  byTenant: Record<string, CostSummary>
  budgets: Budget[]
  cache: CacheStats | null
  tenants: Tenant[]
  range: CostRange
  fetchedAt: string
  primaryHealthy: boolean
  budgetsHealthy: boolean
  cacheHealthy: boolean
}

export type CostRangeKind = "today" | "7d" | "30d" | "90d"

export interface CostRange {
  kind: CostRangeKind
  from: string // ISO
  to: string // ISO
}

export type AnomalySeverity = "info" | "warning" | "critical"

// A single Zone-1 anomaly callout. Severity drives the card border + icon
// colour via tokens; the body is plain text + an optional sparkline series.
export interface CostAnomaly {
  id: string
  severity: AnomalySeverity
  title: string
  context: string
  sparkline?: number[]
  // Where the operator goes when they want to act on the anomaly. Internal
  // navigation only in v1 — external link-outs land via the Adapter pill.
  primaryAction?: { label: string; href: string }
  secondaryAction?: { label: string; href: string }
}

export interface TenantSeriesPoint {
  date: string
  // One key per top-N tenant id + an "other" key. Cost in USD.
  [seriesKey: string]: number | string
}
