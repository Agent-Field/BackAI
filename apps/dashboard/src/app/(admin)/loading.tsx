// SPDX-License-Identifier: Apache-2.0

import { Skeleton } from "@/components/ui/skeleton"

export default function Loading() {
  return (
    <div className="mx-auto flex w-full max-w-[1480px] flex-col gap-4 p-4 md:p-6">
      <div className="flex items-start justify-between">
        <div className="space-y-2">
          <Skeleton className="h-6 w-40" />
          <Skeleton className="h-4 w-96 max-w-full" />
        </div>
        <Skeleton className="h-9 w-32" />
      </div>
      <Skeleton className="h-12 w-full rounded-lg" />
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 8 }).map((_, index) => (
          <Skeleton key={index} className="h-[108px] rounded-lg" />
        ))}
      </div>
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.5fr)_minmax(340px,0.75fr)]">
        <Skeleton className="h-[460px] rounded-lg" />
        <div className="space-y-4">
          <Skeleton className="h-5 w-32" />
          <Skeleton className="h-44 rounded-lg" />
          <Skeleton className="h-44 rounded-lg" />
        </div>
      </div>
    </div>
  )
}
