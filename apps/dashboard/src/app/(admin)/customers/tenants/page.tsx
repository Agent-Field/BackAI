// Tenants page — customer orgs.
//
// Server shell. Checks the multi-tenancy module flag and either falls back
// to the MultiTenancyRequired empty state or renders the live TenantsView.

import { Globe } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import { MultiTenancyRequired } from "@/components/layout/tab-stub"
import { api } from "@/lib/api"

import { TenantsView } from "./_components/tenants-view"

export const dynamic = "force-dynamic"

export default async function Page() {
  const modules = await api.modulesState().catch(() => null)
  if (!modules?.multi_tenancy_enabled) {
    return (
      <MultiTenancyRequired
        title="Tenants"
        description="Your customer orgs"
        icon={Globe}
      />
    )
  }
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Tenants"
        description="Customer orgs. Each tenant has isolated data, secrets, members, and quotas."
      />
      <TenantsView />
    </div>
  )
}
