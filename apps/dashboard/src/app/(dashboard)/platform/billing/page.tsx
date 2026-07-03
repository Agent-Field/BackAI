// SPDX-License-Identifier: Apache-2.0

import { BillingShell } from "@/components/billing-page/billing-shell"
import { fetchBillingSnapshot } from "@/lib/billing-page/data"

// Platform → Billing. Turnkey billing setup: paste Stripe keys (stored
// KMS-encrypted, hot-swapped into the live client — no restart) and
// manage the plans catalog that checkout / entitlements / enforced LLM
// budgets run on. Server-rendered first paint; the shell takes over for
// save/upsert/delete flows and refreshes.

export const dynamic = "force-dynamic"

export default async function BillingPage() {
  const snapshot = await fetchBillingSnapshot()
  return <BillingShell initialSnapshot={snapshot} />
}
