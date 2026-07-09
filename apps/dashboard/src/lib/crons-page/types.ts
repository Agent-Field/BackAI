// SPDX-License-Identifier: Apache-2.0

import type { Cron } from "@/lib/api"

// The active/paused axis is the only filter the crons list carries — the
// runtime's /api/v1/crons endpoint returns every definition, so filtering
// happens client-side in the shell.
export type CronActiveFilter = "all" | "active" | "inactive"

export interface CronsSnapshot {
  crons: Cron[]
  fetchedAt: string
  healthy: boolean
}

export const DEFAULT_CRON_ACTIVE_FILTER: CronActiveFilter = "all"
