// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"
import type { FeatureFlag } from "@/lib/api"

import { sortFlags } from "./derive"

// Server-side fetch for Build → Feature flags. Tolerant of failure: the
// page still mounts (with an explanatory empty state) if the runtime is
// unreachable. Note the list endpoint returns the built-in defaults even
// when no flag database is configured — only writes need persistence — so
// a healthy `ok` here does not guarantee the toggle will stick.

export interface FlagsSnapshot {
  flags: FeatureFlag[]
  fetchedAt: string
  ok: boolean
}

export async function fetchFlagsSnapshot(): Promise<FlagsSnapshot> {
  const fetchedAt = new Date().toISOString()
  const res = await api.config.flags.list().catch(() => null)
  return {
    flags: sortFlags(res?.flags ?? []),
    fetchedAt,
    ok: res !== null,
  }
}
