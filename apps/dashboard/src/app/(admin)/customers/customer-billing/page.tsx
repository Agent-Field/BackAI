import { ReceiptText } from "lucide-react"

import { MultiTenancyRequired } from "@/components/layout/tab-stub"

export default function Page() {
  return (
    <MultiTenancyRequired
      title="Customer Billing"
      description="Per-tenant billing and invoices"
      icon={ReceiptText}


    />
  )
}
