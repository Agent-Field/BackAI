// SPDX-License-Identifier: Apache-2.0

import { Suspense } from "react"
import { TaskDetailView } from "./_task-detail-view"
import { Skeleton } from "@/components/ui/skeleton"

export default async function ShipwrightDetailPage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const { id } = await params
  return (
    <div className="container mx-auto max-w-4xl space-y-6 p-6">
      <Suspense fallback={<Skeleton className="h-96 w-full" />}>
        <TaskDetailView taskId={id} />
      </Suspense>
    </div>
  )
}
