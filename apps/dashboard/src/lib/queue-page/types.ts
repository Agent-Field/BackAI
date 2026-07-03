// SPDX-License-Identifier: Apache-2.0

import type { Job, JobDefinition, QueueSummary } from "@/lib/api"

export type JobState = Job["state"]

export type JobStateFilter = JobState | "all"

export interface QueueSnapshot {
  summary: QueueSummary | null
  jobs: Job[]
  total: number
  hasMore: boolean
  definitions: JobDefinition[]
  fetchedAt: string
  healthy: boolean
}

export const DEFAULT_JOB_STATE_FILTER: JobStateFilter = "all"
