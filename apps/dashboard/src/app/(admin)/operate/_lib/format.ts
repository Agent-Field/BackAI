// Shared formatters for the Operate group.

import { formatDistanceToNow } from "date-fns"

export function formatRelative(ts: string | undefined | null): string {
  if (!ts) return "—"
  try {
    return formatDistanceToNow(new Date(ts), { addSuffix: true })
  } catch {
    return ts
  }
}

export function formatDuration(ms: number | undefined | null): string {
  if (ms === undefined || ms === null) return "—"
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  const mins = Math.floor(ms / 60_000)
  const secs = Math.round((ms % 60_000) / 1000)
  return `${mins}m ${secs}s`
}

export function formatCost(usd: number | undefined | null): string {
  if (usd === undefined || usd === null) return "—"
  const abs = Math.abs(usd)
  if (abs === 0) return "$0.00"
  if (abs < 0.0001) return `$${usd.toFixed(6)}`
  if (abs < 0.01) return `$${usd.toFixed(4)}`
  return `$${usd.toFixed(2)}`
}
