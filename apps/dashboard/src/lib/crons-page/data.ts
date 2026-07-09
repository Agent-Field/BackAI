// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type { CronsSnapshot } from "./types"

// Server-side initial fetch for the Crons page. A single round trip to
// GET /api/v1/crons; a failure degrades to an empty, unhealthy snapshot
// so the page renders its structure (KPIs + table shell) rather than
// crashing. healthy is the "runtime reachable" signal the shell reads.

export async function fetchCronsSnapshot(): Promise<CronsSnapshot> {
  const fetchedAt = new Date().toISOString()
  const list = await api.crons.list().catch(() => null)
  return {
    crons: list?.crons ?? [],
    fetchedAt,
    healthy: list !== null,
  }
}
