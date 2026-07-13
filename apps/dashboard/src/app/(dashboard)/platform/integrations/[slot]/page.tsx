// SPDX-License-Identifier: Apache-2.0

import { redirect } from "next/navigation"

import { IntegrationsShell } from "@/components/integrations-page/integrations-shell"
import { fetchIntegrationsSnapshot } from "@/lib/integrations-page/data"

// Platform → Integrations → <capability>. Focused single-slot form behind
// the sidebar sub-nav (Browser, Sandbox, LLM, ...). "oauth" fans out to
// every oauth_* provider slot. Unknown slugs land back on the overview.

export const dynamic = "force-dynamic"

const KNOWN_SLUGS = new Set([
  "browser",
  "sandbox",
  "llm",
  "notifications",
  "storage",
  "secrets",
  "oauth",
])

interface IntegrationSlotPageProps {
  params: Promise<{ slot: string }>
}

export default async function IntegrationSlotPage({ params }: IntegrationSlotPageProps) {
  const { slot } = await params
  if (!KNOWN_SLUGS.has(slot)) redirect("/platform/integrations")
  const snapshot = await fetchIntegrationsSnapshot()
  return <IntegrationsShell initialSnapshot={snapshot} filter={slot} />
}
