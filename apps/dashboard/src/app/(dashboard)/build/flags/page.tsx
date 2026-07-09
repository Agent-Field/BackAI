// SPDX-License-Identifier: Apache-2.0

import { FlagsShell } from "@/components/flags-page/flags-shell"
import { fetchFlagsSnapshot } from "@/lib/flags-page/data"

// Build → Feature flags. Operator surface for the runtime's feature-flag
// store: list every flag with its built-in default vs. current value, its
// source, and when it last changed, plus an enable/disable toggle per flag
// (PUT /api/v1/config/flags/{key}). Server-rendered first paint; the shell
// takes over for the toggle/refresh flows. Writes need a flag database —
// on a store-less runtime the list still renders but toggles report that
// persistence is unavailable.

export const dynamic = "force-dynamic"

export default async function FlagsPage() {
  const snapshot = await fetchFlagsSnapshot()
  return <FlagsShell initialSnapshot={snapshot} />
}
