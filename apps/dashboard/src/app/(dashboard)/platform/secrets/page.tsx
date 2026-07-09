// SPDX-License-Identifier: Apache-2.0

import { SecretsShell } from "@/components/secrets-page/secrets-shell"
import { fetchSecretsSnapshot } from "@/lib/secrets-page/data"

// Platform → Secrets. Operator surface for the vault-backed secrets store.
// Server-rendered first paint; the shell takes over for the set / rotate /
// reveal / delete flows and refreshes. Plaintext values only ever leave
// the runtime via /reveal — the list and metadata responses never carry a
// value.

export const dynamic = "force-dynamic"

export default async function SecretsPage() {
  const snapshot = await fetchSecretsSnapshot()
  return <SecretsShell initialSnapshot={snapshot} />
}
