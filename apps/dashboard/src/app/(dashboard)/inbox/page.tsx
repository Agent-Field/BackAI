// SPDX-License-Identifier: Apache-2.0

import { fetchInboxSnapshot } from "@/lib/inbox/data"

import { InboxShell } from "@/components/inbox/inbox-shell"

// Server-rendered first paint. Both sources fetch in parallel and tolerate
// failure independently — the page mounts even if approvals or alerts
// returned an error, and the client polling tick recovers the missing
// half on the next round.
export const dynamic = "force-dynamic"

export default async function InboxPage() {
  const snapshot = await fetchInboxSnapshot()
  return <InboxShell initialSnapshot={snapshot} />
}
