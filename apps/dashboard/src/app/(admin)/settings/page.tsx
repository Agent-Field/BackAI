import { Cog } from "lucide-react"

import { ComingSoon } from "@/components/layout/tab-stub"

export default function Page() {
  return (
    <ComingSoon
      title="Settings"
      description="Operator account, dashboard theme, plugins, feature flags"
      icon={Cog}
      bodyTitle="Settings"
      bodyDescription="Configure your operator account and dashboard preferences."
    />
  )
}
