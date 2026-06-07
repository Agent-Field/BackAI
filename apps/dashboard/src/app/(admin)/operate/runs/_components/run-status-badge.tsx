// SPDX-License-Identifier: Apache-2.0

import { Badge } from "@/components/ui/badge"
import type { Run } from "@/lib/api"

// Map run status -> shadcn semantic Badge variant. We avoid raw color
// classes here on purpose: the only "tone" we encode is whether the run
// is in an error state, an active state, or terminal.
const variantByStatus: Record<
  Run["status"],
  React.ComponentProps<typeof Badge>["variant"]
> = {
  queued: "secondary",
  running: "default",
  succeeded: "outline",
  failed: "destructive",
  cancelled: "secondary",
}

const labels: Record<Run["status"], string> = {
  queued: "Queued",
  running: "Running",
  succeeded: "Succeeded",
  failed: "Failed",
  cancelled: "Cancelled",
}

export function RunStatusBadge({ status }: { status: Run["status"] }) {
  return <Badge variant={variantByStatus[status]}>{labels[status]}</Badge>
}