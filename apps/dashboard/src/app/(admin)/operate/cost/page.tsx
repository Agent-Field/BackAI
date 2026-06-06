import { CircleDollarSign } from "lucide-react"

import { ComingSoon } from "@/components/layout/tab-stub"

export default function Page() {
  return (
    <ComingSoon
      title="Cost"
      description="Spend by model, agent, tenant, day"
      icon={CircleDollarSign}
      bodyTitle="No spend recorded"
      bodyDescription="Cost tracking starts once you invoke an LLM."
    />
  )
}
