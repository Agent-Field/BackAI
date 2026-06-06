import { Badge } from "@/components/ui/badge"
import type { QueueSummary } from "@/lib/api"

type JobStatus = QueueSummary["recent"][number]["status"]

const variantByStatus: Record<
  JobStatus,
  React.ComponentProps<typeof Badge>["variant"]
> = {
  pending: "secondary",
  running: "default",
  succeeded: "outline",
  failed: "destructive",
  cancelled: "secondary",
}

const labels: Record<JobStatus, string> = {
  pending: "Pending",
  running: "Running",
  succeeded: "Succeeded",
  failed: "Failed",
  cancelled: "Cancelled",
}

export function QueueStatusBadge({ status }: { status: JobStatus }) {
  return <Badge variant={variantByStatus[status]}>{labels[status]}</Badge>
}
