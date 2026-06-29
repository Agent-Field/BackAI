// SPDX-License-Identifier: Apache-2.0

import { ArrowRight } from "lucide-react"
import Link from "next/link"

import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { api } from "@/lib/api"
import { formatAge } from "@/lib/tenant-detail/derive"

// Minimal Tenants list (v1). A full list page with filters / search /
// columns lives in a future brief; for now this hub renders every
// tenant the runtime returns and links to the rich Tenant detail
// surface. Empty state explains how to seed a tenant via the demo
// seeder.

export const dynamic = "force-dynamic"

export default async function TenantsListPage() {
  const list = await api.admin.tenants.list().catch(() => null)
  const tenants = list?.tenants ?? []
  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex items-baseline justify-between">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">
            Tenants
          </h1>
          <p className="text-meta text-muted-foreground">
            Every tenant on this fork. Click through for KPIs, members,
            keys, and operator actions.
          </p>
        </div>
      </header>
      <ZoneCard>
        <ZoneCardHeader
          title="All tenants"
          subtitle={`${tenants.length} configured`}
        />
        {tenants.length === 0 ? (
          <p className="px-row-x py-tile text-meta text-muted-foreground">
            No tenants returned from the runtime. Seed the demo dataset
            with{" "}
            <code className="font-mono">./scripts/demo-traffic.sh seed</code>{" "}
            or invite a customer via{" "}
            <code className="font-mono">POST /api/v1/admin/tenants</code>.
          </p>
        ) : (
          <ul role="list" className="divide-y">
            {tenants.map((t) => (
              <li key={t.id}>
                <Link
                  href={`/people/tenants/${encodeURIComponent(t.id)}`}
                  className="group grid grid-cols-[minmax(0,1fr)_minmax(120px,1fr)_120px_18px] items-center gap-stack px-row-x py-row-y text-meta transition-colors hover:bg-accent/40"
                >
                  <div className="flex min-w-0 flex-col gap-tile-tight">
                    <span className="truncate text-body font-medium text-foreground">
                      {t.name || t.slug}
                    </span>
                    <span className="truncate font-mono text-meta text-muted-foreground">
                      {t.slug}
                    </span>
                  </div>
                  <span className="truncate font-mono text-meta text-muted-foreground" title={t.id}>
                    {t.id.slice(0, 8)}
                  </span>
                  <span className="text-right font-mono text-meta tabular-nums text-muted-foreground">
                    {formatAge(t.created_at)}
                  </span>
                  <ArrowRight
                    aria-hidden
                    className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
                  />
                </Link>
              </li>
            ))}
          </ul>
        )}
      </ZoneCard>
    </div>
  )
}
