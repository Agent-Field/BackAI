// SPDX-License-Identifier: Apache-2.0

import "server-only"

import { api } from "@/lib/api"

import type { WebhookDirectionFilter, WebhooksSnapshot } from "./types"

// Server-side initial fetch for the Webhooks page. Endpoints + the
// delivery feed in parallel; each degrades independently so one flaky
// endpoint doesn't blank the whole page. healthy flips false only when
// both failed — that's the "runtime down" signal.

export async function fetchWebhooksSnapshot(
  direction: WebhookDirectionFilter = "all",
): Promise<WebhooksSnapshot> {
  const fetchedAt = new Date().toISOString()
  const [endpoints, deliveries] = await Promise.all([
    api.webhooks.endpoints().catch(() => null),
    api.webhooks
      .deliveries({
        limit: 100,
        direction: direction === "all" ? undefined : direction,
      })
      .catch(() => null),
  ])

  return {
    endpoints: endpoints?.endpoints ?? [],
    deliveries: deliveries?.deliveries ?? [],
    total: deliveries?.total ?? 0,
    hasMore: deliveries?.has_more ?? false,
    fetchedAt,
    healthy: endpoints !== null || deliveries !== null,
  }
}
