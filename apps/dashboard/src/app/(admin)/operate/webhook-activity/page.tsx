import { Activity } from "lucide-react"

import { ComingSoon } from "@/components/layout/tab-stub"

export default function Page() {
  return (
    <ComingSoon
      title="Webhook Activity"
      description="Recent incoming and outgoing deliveries"
      icon={Activity}
      bodyTitle="Webhooks module disabled"
      bodyDescription="Webhook delivery log lands in Phase 10."
    />
  )
}
