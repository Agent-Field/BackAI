// SPDX-License-Identifier: Apache-2.0

"use client"

import { useRouter } from "next/navigation"
import { useTransition } from "react"
import { CheckCircle2, RotateCcw, VolumeX } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"

export function ErrorGroupActions({ groupId, status }: { groupId: string; status: string }) {
  const router = useRouter()
  const [pending, startTransition] = useTransition()

  const run = (action: "mute" | "resolve" | "reopen") => {
    startTransition(async () => {
      try {
        if (action === "mute") await api.errors.mute(groupId)
        if (action === "resolve") await api.errors.resolve(groupId)
        if (action === "reopen") await api.errors.reopen(groupId)
        toast.success(`Error group ${action === "reopen" ? "reopened" : `${action}d`}.`)
        router.refresh()
      } catch (err) {
        toast.error("Could not update error group", {
          description: err instanceof Error ? err.message : "The active errors adapter rejected the action.",
        })
      }
    })
  }

  return (
    <div className="mt-1 flex flex-wrap items-center gap-1">
      {status !== "muted" && (
        <Button type="button" size="sm" variant="ghost" className="h-6 gap-1 px-2 text-xs" disabled={pending} onClick={() => run("mute")}>
          <VolumeX className="size-3" />
          Mute
        </Button>
      )}
      {status !== "resolved" && (
        <Button type="button" size="sm" variant="ghost" className="h-6 gap-1 px-2 text-xs" disabled={pending} onClick={() => run("resolve")}>
          <CheckCircle2 className="size-3" />
          Resolve
        </Button>
      )}
      {status !== "open" && (
        <Button type="button" size="sm" variant="ghost" className="h-6 gap-1 px-2 text-xs" disabled={pending} onClick={() => run("reopen")}>
          <RotateCcw className="size-3" />
          Reopen
        </Button>
      )}
    </div>
  )
}
