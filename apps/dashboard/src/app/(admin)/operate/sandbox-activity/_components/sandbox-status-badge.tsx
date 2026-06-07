import { Badge } from "@/components/ui/badge"
import type { SandboxRun } from "@/lib/api"

// Map sandbox run status -> shadcn semantic Badge variant. The mapping
// follows the same conventions as the runs page so operators don't have
// to relearn what each color means.
const variantByStatus: Record<
  SandboxRun["status"],
  React.ComponentProps<typeof Badge>["variant"]
> = {
  queued: "secondary",
  running: "default",
  done: "outline",
  failed: "destructive",
  timeout: "destructive",
  killed: "secondary",
}

const labels: Record<SandboxRun["status"], string> = {
  queued: "Queued",
  running: "Running",
  done: "Done",
  failed: "Failed",
  timeout: "Timeout",
  killed: "Killed",
}

export function SandboxStatusBadge({ status }: { status: SandboxRun["status"] }) {
  return <Badge variant={variantByStatus[status]}>{labels[status]}</Badge>
}
