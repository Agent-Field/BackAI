import { Activity } from "lucide-react"

import { ComingSoon } from "@/components/layout/tab-stub"

export default function Page() {
  return (
    <ComingSoon
      title="Sandbox Activity"
      description="Live pool and recent sandbox runs"
      icon={Activity}
      bodyTitle="Sandbox module disabled"
      bodyDescription="Enable the sandbox module to see activity."
    />
  )
}
