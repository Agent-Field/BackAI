// Streaming skeleton for the Cost dashboard.

import { PageHeader } from "@/components/layout/page-header"
import { Card, CardHeader } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"

export default function CostLoading() {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Cost"
        description="Spend by model, agent, tenant, day. Budget alerts and forecast."
      />

      <div className="flex flex-wrap items-center gap-2">
        <Skeleton className="h-8 w-32" />
        <Skeleton className="h-8 w-36" />
        <Skeleton className="h-8 w-36" />
        <Skeleton className="h-8 w-36" />
        <div className="ml-auto">
          <Skeleton className="h-8 w-32" />
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Card key={i}>
            <CardHeader>
              <Skeleton className="h-3 w-20" />
              <Skeleton className="mt-2 h-9 w-32" />
              <Skeleton className="mt-3 h-3 w-40" />
            </CardHeader>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <Skeleton className="h-4 w-28" />
          <Skeleton className="mt-1 h-3 w-64" />
        </CardHeader>
        <div className="px-4 pb-4">
          <Skeleton className="h-[320px] w-full" />
        </div>
      </Card>

      <Card>
        <CardHeader>
          <Skeleton className="h-4 w-28" />
          <Skeleton className="mt-1 h-3 w-64" />
        </CardHeader>
        <div className="flex flex-col gap-2 px-4 pb-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      </Card>
    </div>
  )
}
