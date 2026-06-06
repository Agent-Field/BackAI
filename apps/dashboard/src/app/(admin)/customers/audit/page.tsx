import { FileBadge } from "lucide-react"

import { MultiTenancyRequired } from "@/components/layout/tab-stub"

export default function Page() {
  return (
    <MultiTenancyRequired
      title="Audit"
      description="Who did what when"
      icon={FileBadge}


    />
  )
}
