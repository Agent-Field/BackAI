// SPDX-License-Identifier: Apache-2.0

import { CronsShell } from "@/components/crons-page/crons-shell"
import { fetchCronsSnapshot } from "@/lib/crons-page/data"

// Build → Crons. Server-rendered first paint of the scheduled-job roster;
// the shell takes over for live polling, the URL-driven active filter,
// inline row expansion, and the Trigger / Pause mutations.

export const dynamic = "force-dynamic"

export default async function CronsPage() {
  const snapshot = await fetchCronsSnapshot()
  return <CronsShell initialSnapshot={snapshot} />
}
