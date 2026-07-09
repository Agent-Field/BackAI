// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type { OAuthProviderFilter, OAuthSnapshot } from "./types"

// Server-side initial fetch for People → OAuth. Providers, the active
// connections list, and the token-refresh feed load in parallel; each
// degrades independently so one flaky call doesn't blank the page.
// healthy flips false only when both the providers and connections calls
// failed — that's the "runtime down / no oauth scope" signal. The
// provider filter narrows only the refresh feed, matching the URL param
// the shell drives.

export async function fetchOAuthSnapshot(
  provider: OAuthProviderFilter = "",
): Promise<OAuthSnapshot> {
  const fetchedAt = new Date().toISOString()
  const [providers, connections, history] = await Promise.all([
    api.oauth.providers().catch(() => null),
    api.oauth.connections().catch(() => null),
    api.oauth.refreshHistory({ provider: provider || undefined, limit: 100 }).catch(() => null),
  ])

  return {
    providers: providers?.providers ?? [],
    connections: connections?.connections ?? [],
    events: history?.events ?? [],
    fetchedAt,
    healthy: providers !== null || connections !== null,
  }
}
