// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import { windowFromISO } from "./derive"
import type { ReasonersSnapshot, ReasonerWindowKind } from "./types"

// Server-side fetch for Build → Reasoners. The window kind becomes a
// `from` ISO timestamp; the agent filter (?agent=) is applied to the
// fetched rows here — the analytics endpoint has no agent param yet and
// the row universe is small (limit 200). Failures degrade to
// `healthy: false` so the page shows a notice, not a crash.

export async function fetchReasonersSnapshot(
  windowKind: ReasonerWindowKind = "24h",
  agent?: string,
): Promise<ReasonersSnapshot> {
  const fetchedAt = new Date().toISOString()
  const result = await api.reasoners
    .analytics({ from: windowFromISO(windowKind), limit: 200 })
    .catch(() => null)

  if (result === null) {
    return { rows: [], window: null, fetchedAt, healthy: false }
  }

  const rows = agent
    ? result.reasoners.filter((row) => row.agent === agent)
    : result.reasoners

  return { rows, window: result.window, fetchedAt, healthy: true }
}
