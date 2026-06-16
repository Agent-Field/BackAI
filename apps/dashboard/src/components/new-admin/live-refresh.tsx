// SPDX-License-Identifier: Apache-2.0

"use client"

import { useEffect } from "react"
import { useRouter } from "next/navigation"

export function LiveRefresh({
  enabled,
  intervalMs = 15_000,
}: {
  enabled: boolean
  intervalMs?: number
}) {
  const router = useRouter()

  useEffect(() => {
    if (!enabled) return

    const refresh = () => {
      if (document.visibilityState === "visible") {
        router.refresh()
      }
    }

    const timer = window.setInterval(refresh, intervalMs)
    document.addEventListener("visibilitychange", refresh)

    return () => {
      window.clearInterval(timer)
      document.removeEventListener("visibilitychange", refresh)
    }
  }, [enabled, intervalMs, router])

  return null
}
