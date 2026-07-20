// SPDX-License-Identifier: Apache-2.0

// Pure formatting helpers for the Errors page. Kept dependency-free so
// both the server page and the client shell can import them.

/** Compact relative age for first_seen / last_seen columns. */
export function formatErrorAge(iso: string | null): string {
  if (!iso) return "—"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return "—"
  const diffMs = Math.max(0, Date.now() - ts)
  if (diffMs < 1000) return "now"
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86_400) return `${Math.floor(sec / 3600)}h ago`
  return new Date(ts).toISOString().slice(5, 16).replace("T", " ")
}

/** Compact count formatting so wide numbers don't blow the column. */
export function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 10_000) return `${Math.round(n / 1000)}k`
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(n)
}
