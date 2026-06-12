// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import { ExternalLink } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"

type Props = {
  tenantId: string
}

export function ManageSubscriptionButton({ tenantId }: Props) {
  const [pending, setPending] = useState(false)

  const handleClick = async () => {
    setPending(true)
    try {
      const res = await fetch(`/api/v1/billing/customers/${encodeURIComponent(tenantId)}/portal`, {
        method: "POST",
        credentials: "include",
      })
      if (!res.ok) {
        toast.error("Subscription management is not available for this demo account.")
        return
      }
      const data = (await res.json()) as { url?: string }
      if (data.url) {
        window.open(data.url, "_blank", "noopener,noreferrer")
      } else {
        toast.error("Subscription management is not available for this demo account.")
      }
    } catch (err) {
      toast.error("Subscription management is not available right now.")
    } finally {
      setPending(false)
    }
  }

  return (
    <Button onClick={handleClick} disabled={pending}>
      <ExternalLink data-icon="inline-start" />
      {pending ? "Opening..." : "Manage subscription"}
    </Button>
  )
}
