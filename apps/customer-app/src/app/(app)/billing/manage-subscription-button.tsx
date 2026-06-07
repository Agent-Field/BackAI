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
      const res = await fetch(
        `/api/v1/billing/customers/${encodeURIComponent(tenantId)}/portal`,
        { method: "POST", credentials: "include" },
      )
      if (!res.ok) {
        const text = await res.text()
        toast.error(`Could not open portal: ${text.slice(0, 200)}`)
        return
      }
      const data = (await res.json()) as { url?: string }
      if (data.url) {
        window.open(data.url, "_blank", "noopener,noreferrer")
      } else {
        toast.error("Portal URL missing in response.")
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
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
