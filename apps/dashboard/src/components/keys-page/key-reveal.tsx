// SPDX-License-Identifier: Apache-2.0

"use client"

import { TriangleAlert } from "lucide-react"

import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

import type { IssuedAPIKey } from "@/lib/api"

// One-time key reveal. The runtime returns the plaintext `value` exactly
// once (on issue and on rotate) — this block mirrors the inline reveal
// in home/quick-actions.tsx: mono value + copy + a can't-miss warning.

export function KeyValueBlock({ value }: { value: string }) {
  return (
    <div className="flex flex-col gap-tile-tight">
      <div className="flex items-center gap-inline">
        <code className="min-w-0 flex-1 truncate rounded-md border bg-muted/40 px-1.5 py-0.5 font-mono text-meta text-muted-foreground">
          {value}
        </code>
        <CopyButton value={value} label="Copy API key" />
      </div>
      <p className="flex items-center gap-inline text-meta text-warning">
        <TriangleAlert className="size-3.5 shrink-0" aria-hidden />
        Copy it now — you won&apos;t see this value again.
      </p>
    </div>
  )
}

// Standalone dialog wrapper used by the rotate flow (issue has its own
// dialog with a result phase). Parent owns open state via `issued`.

interface KeyRevealDialogProps {
  issued: IssuedAPIKey | null
  title: string
  onClose: () => void
}

export function KeyRevealDialog({ issued, title, onClose }: KeyRevealDialogProps) {
  return (
    <Dialog open={issued !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {issued?.name
              ? `New value for “${issued.name}” (${issued.prefix}…).`
              : issued
                ? `New value for ${issued.prefix}…`
                : ""}
          </DialogDescription>
        </DialogHeader>
        <div className="px-4 pb-1">
          {issued ? <KeyValueBlock value={issued.value} /> : null}
        </div>
        <DialogFooter className="flex-row justify-end gap-inline pt-stack">
          <Button type="button" size="sm" onClick={onClose}>
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
