// SPDX-License-Identifier: Apache-2.0

import type { OAuthConnection, OAuthProvider, OAuthRefreshHistory } from "@/lib/api"

// The refresh-history event type isn't exported on its own — pull it off
// the list schema so the page and its cards share one shape.
export type OAuthRefreshEvent = OAuthRefreshHistory["events"][number]

// "" means the "All" chip — no provider filter on the refresh feed.
export type OAuthProviderFilter = string

export const DEFAULT_PROVIDER_FILTER: OAuthProviderFilter = ""

export interface OAuthSnapshot {
  providers: OAuthProvider[]
  connections: OAuthConnection[]
  events: OAuthRefreshEvent[]
  fetchedAt: string
  // false only when the providers AND connections calls both failed —
  // the "runtime unreachable / missing oauth scope" signal.
  healthy: boolean
}
