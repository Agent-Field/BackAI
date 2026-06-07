// Tenant drilldown page — /customers/tenants/[id].
//
// Server shell. Fetches the Phase 12.1 drilldown payload + audit log in
// parallel (Promise.allSettled — a failure in either still renders the
// page) and hands them to the client view. Tolerant by design: when
// multi-tenancy is off we fall back to the same empty state used by the
// tenants list page.

import { notFound } from "next/navigation"
import { Globe } from "lucide-react"

import { MultiTenancyRequired } from "@/components/layout/tab-stub"
import { api, type AuditEntry, type TenantDrilldown } from "@/lib/api"

import { TenantDrilldownView } from "./_components/tenant-drilldown-view"

export const dynamic = "force-dynamic"

type PageProps = {
  // Next.js 15 — params is async.
  params: Promise<{ id: string }>
}

export default async function Page({ params }: PageProps) {
  const { id } = await params

  const modules = await api.modulesState().catch(() => null)
  if (!modules?.multi_tenancy_enabled) {
    return (
      <MultiTenancyRequired
        title="Tenant detail"
        description="Per-customer drilldown"
        icon={Globe}
      />
    )
  }

  // Both fetches degrade independently — audit is a "nice to have"
  // panel and shouldn't take down the whole drilldown.
  const [drilldownResult, auditResult] = await Promise.allSettled([
    api.tenantDrilldown(id),
    api.admin.audit.list({ tenant: id, limit: 10 }),
  ])

  if (drilldownResult.status === "rejected") {
    // 404 means the tenant id is bad — let Next render the not-found
    // page rather than a confusing error state.
    const message =
      drilldownResult.reason instanceof Error
        ? drilldownResult.reason.message
        : String(drilldownResult.reason)
    if (message.includes("NOT_FOUND") || message.includes("404")) {
      notFound()
    }
    return (
      <TenantDrilldownView
        drilldown={null}
        audit={[]}
        loadError={message}
        tenantId={id}
      />
    )
  }

  const audit: AuditEntry[] =
    auditResult.status === "fulfilled" ? auditResult.value.entries : []
  const drilldown: TenantDrilldown = drilldownResult.value

  return (
    <TenantDrilldownView
      drilldown={drilldown}
      audit={audit}
      loadError={null}
      tenantId={id}
    />
  )
}
