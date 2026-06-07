// SPDX-License-Identifier: Apache-2.0

// API Keys page — issue and revoke per-tenant programmatic credentials.

import { KeyRound } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import { MultiTenancyRequired } from "@/components/layout/tab-stub"
import { api } from "@/lib/api"

import { APIKeysView } from "./_components/api-keys-view"

export const dynamic = "force-dynamic"

export default async function Page() {
  const modules = await api.modulesState().catch(() => null)
  if (!modules?.multi_tenancy_enabled) {
    return (
      <MultiTenancyRequired
        title="API Keys"
        description="Keys issued to your customers"
        icon={KeyRound}
      />
    )
  }
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="API Keys"
        description="Programmatic credentials scoped to a single tenant. Values are shown once at issue time."
      />
      <APIKeysView />
    </div>
  )
}