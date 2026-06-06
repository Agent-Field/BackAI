import { Users } from "lucide-react"

import { MultiTenancyRequired } from "@/components/layout/tab-stub"

export default function Page() {
  return (
    <MultiTenancyRequired
      title="Users"
      description="End users across tenants"
      icon={Users}


    />
  )
}
