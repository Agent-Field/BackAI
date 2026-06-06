import { Workflow } from "lucide-react"

import { ComingSoon } from "@/components/layout/tab-stub"

export default function Page() {
  return (
    <ComingSoon
      title="Runs"
      description="Agent executions with logs and traces"
      icon={Workflow}
      bodyTitle="No runs yet"
      bodyDescription="Invoke an agent to see runs here."
    />
  )
}
