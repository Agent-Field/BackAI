import { KeyRound } from "lucide-react"

import { MultiTenancyRequired } from "@/components/layout/tab-stub"

export default function Page() {
  return (
    <MultiTenancyRequired
      title="API Keys"
      description="Keys issued to your customers"
      icon={KeyRound}


    />
  )
}
