// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"
import type { AdapterRegistrySlot } from "@/lib/api"

// Server-side fetch for the adapter registry. Tolerant of failure: the page
// still mounts (with an explanatory empty state) if the runtime is unreachable.

export interface AdaptersSnapshot {
  slots: AdapterRegistrySlot[]
  fetchedAt: string
  ok: boolean
}

export async function fetchAdaptersSnapshot(): Promise<AdaptersSnapshot> {
  const fetchedAt = new Date().toISOString()
  const res = await api.admin.adapters.list().catch(() => null)
  const slots = (res?.slots ?? [])
    .slice()
    .sort((a, b) => a.tier - b.tier || a.slot.localeCompare(b.slot))
  return { slots, fetchedAt, ok: res !== null }
}
