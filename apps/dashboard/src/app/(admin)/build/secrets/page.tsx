// Secrets page — vault entries.
//
// Client component (in the form of a wrapping server page) so the inner
// SecretsView owns Dialog and AlertDialog state. The list endpoint never
// returns the secret value — reveal is gated behind an explicit dialog.

import { PageHeader } from "@/components/layout/page-header"

import { SecretsView } from "./_components/secrets-view"

export const dynamic = "force-dynamic"

export default function Page() {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Secrets"
        description="Per-tenant or global secrets vault. Values are stored encrypted and only revealed via explicit action."
      />
      <SecretsView />
    </div>
  )
}
