// SPDX-License-Identifier: Apache-2.0

import type { LogLine } from "@/lib/api"

// Pure helpers shared by the server snapshot and the client shell.

/**
 * Newest-first ordering. The runtime's in-memory ring returns entries
 * chronologically (oldest first — see services/runtime
 * internal/logger/ring.go Recent); external adapters may differ, so we
 * sort defensively instead of just reversing. Stable for equal
 * timestamps, so intra-second ordering from the source is preserved.
 */
export function sortNewestFirst(logs: LogLine[]): LogLine[] {
  return logs
    .map((line, i) => ({ line, i, ts: Date.parse(line.ts) }))
    .sort((a, b) => {
      const at = Number.isNaN(a.ts) ? 0 : a.ts
      const bt = Number.isNaN(b.ts) ? 0 : b.ts
      if (at !== bt) return bt - at
      return b.i - a.i
    })
    .map((entry) => entry.line)
}

/** Local wall-clock time for the timestamp column (HH:MM:SS). */
export function formatLogTime(iso: string): string {
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  return new Date(ts).toLocaleTimeString(undefined, { hour12: false })
}

/**
 * Structured metadata carried by a log line beyond the headline
 * columns. Rendered as pretty-printed JSON in the expanded row.
 */
export function logLineDetails(line: LogLine): Record<string, string> {
  const details: Record<string, string> = {}
  if (line.request_id) details.request_id = line.request_id
  if (line.tenant_id) details.tenant_id = line.tenant_id
  if (line.agent) details.agent = line.agent
  return details
}
