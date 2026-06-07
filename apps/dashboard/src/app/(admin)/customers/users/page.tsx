// Users page — end users across tenants.

import { Users } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import { MultiTenancyRequired } from "@/components/layout/tab-stub"
import { api } from "@/lib/api"

import { UsersView } from "./_components/users-view"

export const dynamic = "force-dynamic"

export default async function Page() {
  const modules = await api.modulesState().catch(() => null)
  if (!modules?.multi_tenancy_enabled) {
    return (
      <MultiTenancyRequired
        title="Users"
        description="End users across tenants"
        icon={Users}
      />
    )
  }
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Users"
        description="Read-only directory of end users across all tenants. Membership management is on the tenant detail."
      />
      <UsersView />
    </div>
  )
}
