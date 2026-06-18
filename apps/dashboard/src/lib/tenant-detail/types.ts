// SPDX-License-Identifier: Apache-2.0

import type { TenantDrilldown } from "@/lib/api"

export type TenantDetailTab =
  | "overview"
  | "members"
  | "keys"
  | "settings"

export const DEFAULT_TAB: TenantDetailTab = "overview"

export interface TenantDetailSnapshot {
  drilldown: TenantDrilldown
  fetchedAt: string
}

export type AtRiskSeverity = "warning" | "critical"

// Composite "this tenant needs attention" signal. Computed client-side
// from the drilldown payload until Gap 33 lands a backend health score.
// The header renders one AtRiskAlert per signal — multiple stack.
export interface AtRiskSignal {
  id: string
  severity: AtRiskSeverity
  message: string
  detail?: string
}

export function isTenantDetailTab(value: string): value is TenantDetailTab {
  return (
    value === "overview" ||
    value === "members" ||
    value === "keys" ||
    value === "settings"
  )
}
