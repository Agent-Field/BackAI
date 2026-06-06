import { ListChecks } from "lucide-react"

import { ComingSoon } from "@/components/layout/tab-stub"

export default function Page() {
  return (
    <ComingSoon
      title="Queues"
      description="Live job queue state"
      icon={ListChecks}
      bodyTitle="Queue is empty"
      bodyDescription="Job queues land in Phase 5."
    />
  )
}
