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

import type { SecretValue } from "@/lib/api"

// Secret reveal. Plaintext leaves the runtime only via POST /reveal — this
// block mirrors the one-time key reveal in keys-page/key-reveal.tsx: mono
// value + copy + a can't-miss warning. Unlike an issued key the value is
// stable, but we still treat it as sensitive: no autofocus, no persistence.

export function SecretValueBlock({ value }: { value: string }) {
  return (
    <div className="flex flex-col gap-tile-tight">
      <div className="flex items-center gap-inline">
        <code className="min-w-0 flex-1 truncate rounded-md border bg-muted/40 px-1.5 py-0.5 font-mono text-meta text-muted-foreground">
          {value}
        </code>
        <CopyButton value={value} label="Copy secret value" />
      </div>
      <p className="flex items-center gap-inline text-meta text-warning">
        <TriangleAlert className="size-3.5 shrink-0" aria-hidden />
        Handle with care — this is the live plaintext value.
      </p>
    </div>
  )
}

// Standalone reveal dialog. Parent owns open state via `revealed` (the
// SecretValue returned by the row's reveal call).

interface RevealSecretDialogProps {
  revealed: SecretValue | null
  onClose: () => void
}

export function RevealSecretDialog({ revealed, onClose }: RevealSecretDialogProps) {
  return (
    <Dialog open={revealed !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Secret value</DialogTitle>
          <DialogDescription>
            {revealed ? `Plaintext for “${revealed.key}”.` : ""}
          </DialogDescription>
        </DialogHeader>
        <div className="px-4 pb-1">
          {revealed ? <SecretValueBlock value={revealed.value} /> : null}
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
