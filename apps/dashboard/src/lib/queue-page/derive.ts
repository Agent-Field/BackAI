// SPDX-License-Identifier: Apache-2.0

import type { Job } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"

import type { JobState, JobStateFilter } from "./types"

// Status helpers ---------------------------------------------------------

// River job states, mapped onto the dashboard's four-tone status scale.
// retryable/discarded read as "act" (something failed), running/available/
// scheduled/pending read as "watch" (in flight), completed is "ok".
export function classifyJobState(state: JobState): StatusState {
  switch (state) {
    case "completed":
      return "ok"
    case "running":
    case "available":
    case "scheduled":
    case "pending":
      return "watch"
    case "retryable":
    case "discarded":
      return "act"
    case "cancelled":
      return "idle"
  }
}

export function isJobState(v: string): v is JobState {
  return (
    v === "available" ||
    v === "running" ||
    v === "completed" ||
    v === "discarded" ||
    v === "cancelled" ||
    v === "retryable" ||
    v === "scheduled" ||
    v === "pending"
  )
}

export function isJobStateFilter(v: string): v is JobStateFilter {
  return v === "all" || isJobState(v)
}

// Retry is only meaningful for jobs that failed their way out of the
// queue — retryable (waiting for backoff) and discarded (gave up).
export function isRetryableJob(job: Job): boolean {
  return job.state === "retryable" || job.state === "discarded"
}

export function lastJobError(job: Job): string | null {
  if (!job.errors || job.errors.length === 0) return null
  return job.errors[job.errors.length - 1]?.error ?? null
}

// Formatters -------------------------------------------------------------

export function formatJobAge(iso: string | null): string {
  if (!iso) return "—"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = Math.max(0, Date.now() - ts)
  if (diffMs < 1000) return "now"
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86_400) return `${Math.floor(sec / 3600)}h ago`
  return new Date(ts).toISOString().slice(5, 16).replace("T", " ")
}

export function formatAttempts(job: Job): string {
  return `${job.attempt}/${job.max_attempts}`
}

export function shortJobId(id: string): string {
  return id.length > 12 ? id.slice(0, 12) : id
}

export function safeStringifyArgs(value: unknown): string {
  if (value === undefined || value === null) return ""
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}
