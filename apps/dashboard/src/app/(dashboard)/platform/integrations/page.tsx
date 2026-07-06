// SPDX-License-Identifier: Apache-2.0

import { IntegrationsShell } from "@/components/integrations-page/integrations-shell"
import { fetchIntegrationsSnapshot } from "@/lib/integrations-page/data"

// Platform → Integrations. Operator surface for adapter credentials:
// email/Resend, Slack, Twilio SMS, FCM push, and the remote
// storage/secrets/LLM adapter URLs + tokens. Secrets are stored server-
// side (vault-backed) and never echoed back — the page renders a masked
// fingerprint + a "set" indicator per field. Server-rendered first paint;
// the shell takes over for save/clear flows and refreshes.

export const dynamic = "force-dynamic"

export default async function IntegrationsPage() {
  const snapshot = await fetchIntegrationsSnapshot()
  return <IntegrationsShell initialSnapshot={snapshot} />
}
