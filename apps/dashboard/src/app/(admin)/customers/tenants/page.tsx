import { Globe } from "lucide-react"

import { MultiTenancyRequired } from "@/components/layout/tab-stub"

export default function Page() {
  return (
    <MultiTenancyRequired
      title="Tenants"
      description="Your customer orgs"
      icon={Globe}


    />
  )
}
