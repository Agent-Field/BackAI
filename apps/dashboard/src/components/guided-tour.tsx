// SPDX-License-Identifier: Apache-2.0

"use client"

import { useEffect } from "react"
import { driver, type DriveStep } from "driver.js"
import { Route } from "lucide-react"

import { Button } from "@/components/ui/button"

type Props = {
  id: string
  steps: DriveStep[]
  autoStart?: boolean
  label?: string
}

export function GuidedTour({
  id,
  steps,
  autoStart = false,
  label = "Guided walkthrough",
}: Props) {
  const storageKey = `backai:tour:${id}`

  const start = () => {
    const tour = driver({
      showProgress: true,
      allowClose: true,
      animate: true,
      overlayOpacity: 0.62,
      stagePadding: 8,
      popoverClass: "backai-driver-popover",
      nextBtnText: "Next",
      prevBtnText: "Back",
      doneBtnText: "Done",
      steps,
      onDestroyed: () => {
        try {
          window.localStorage.setItem(storageKey, "done")
        } catch {
          // Storage is best-effort only.
        }
      },
    })
    tour.drive()
  }

  useEffect(() => {
    if (!autoStart) return
    try {
      if (window.localStorage.getItem(storageKey) === "done") return
    } catch {
      return
    }
    const timer = window.setTimeout(start, 600)
    return () => window.clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoStart, storageKey])

  return (
    <Button variant="outline" size="sm" onClick={start}>
      <Route data-icon="inline-start" />
      {label}
    </Button>
  )
}
