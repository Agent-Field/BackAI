// SPDX-License-Identifier: Apache-2.0

import { WebhooksShell } from "@/components/webhooks-page/webhooks-shell"
import { fetchWebhooksSnapshot } from "@/lib/webhooks-page/data"
import { isWebhookDirectionFilter } from "@/lib/webhooks-page/derive"
import type { WebhookDirectionFilter } from "@/lib/webhooks-page/types"

// Server-rendered first paint. Shell takes over for live polling +
// URL-driven direction filter + endpoint create/delete dialogs.

export const dynamic = "force-dynamic"

export default async function WebhooksPage({
  searchParams,
}: {
  searchParams: Promise<{ direction?: string }>
}) {
  const sp = await searchParams
  const direction: WebhookDirectionFilter =
    sp.direction && isWebhookDirectionFilter(sp.direction)
      ? sp.direction
      : "all"
  const snapshot = await fetchWebhooksSnapshot(direction)
  return <WebhooksShell initialSnapshot={snapshot} />
}
