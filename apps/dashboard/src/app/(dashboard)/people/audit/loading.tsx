// SPDX-License-Identifier: Apache-2.0

import { Skeleton } from "@/components/ui/skeleton"
import { ZoneCard } from "@/components/ui/zone-card"

// Route-level skeleton for People → Audit. Shown while the server page
// fetches; mirrors the header / filter bar / table card shell so the
// layout doesn't jump on load.

export default function AuditLoading() {
  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <div className="flex flex-col gap-tile-tight">
        <Skeleton className="h-6 w-32" />
        <Skeleton className="h-3 w-80" />
      </div>
      <Skeleton className="h-11 w-full rounded-md" />
      <ZoneCard>
        <div className="flex flex-col gap-stack px-row-x py-row-y">
          {Array.from({ length: 10 }).map((_, i) => (
            <Skeleton key={i} className="h-7 w-full" />
          ))}
        </div>
      </ZoneCard>
    </div>
  )
}
