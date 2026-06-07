// SPDX-License-Identifier: Apache-2.0

// Audit page — who did what when.

import { FileBadge } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import { MultiTenancyRequired } from "@/components/layout/tab-stub"
import { api } from "@/lib/api"

import { AuditView } from "./_components/audit-view"

export const dynamic = "force-dynamic"

export default async function Page() {
  const modules = await api.modulesState().catch(() => null)
  if (!modules?.multi_tenancy_enabled) {
    return (
      <MultiTenancyRequired
        title="Audit"
        description="Who did what when"
        icon={FileBadge}
      />
    )
  }
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Audit"
        description="Append-only log of admin actions across tenants, users, keys, and resources."
      />
      <AuditView />
    </div>
  )
}