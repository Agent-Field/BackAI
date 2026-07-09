// SPDX-License-Identifier: Apache-2.0

"use client"

import { Plus, RefreshCw } from "lucide-react"
import { useCallback, useState } from "react"

import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import type { SecretMetadata, SecretValue } from "@/lib/api"
import type { SecretsSnapshot } from "@/lib/secrets-page/types"

import { RevealSecretDialog } from "./secret-reveal"
import { RotateSecretDialog } from "./rotate-secret-dialog"
import { SecretsTable } from "./secrets-table"
import { SetSecretDialog } from "./set-secret-dialog"

// SecretsShell owns:
//   - Client refetch after set / rotate / delete mutations + a manual refresh
//   - The set dialog (create when target is null, edit otherwise)
//   - The rotate dialog and the one-off reveal dialog
// The vault is single-tenant today, so there is no tenant filter — the
// list reflects the runtime's default tenant.

interface SecretsShellProps {
  initialSnapshot: SecretsSnapshot
}

export function SecretsShell({ initialSnapshot }: SecretsShellProps) {
  const [snapshot, setSnapshot] = useState<SecretsSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)
  const [setOpen, setSetOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<SecretMetadata | null>(null)
  const [rotateTarget, setRotateTarget] = useState<SecretMetadata | null>(null)
  const [revealed, setRevealed] = useState<SecretValue | null>(null)

  const refresh = useCallback(async (manual = false) => {
    if (manual) setRefreshing(true)
    const fetchedAt = new Date().toISOString()
    try {
      const list = await api.secrets.list()
      setSnapshot({ secrets: list.secrets, fetchedAt, healthy: true })
    } catch {
      setSnapshot((prev) => ({ ...prev, fetchedAt, healthy: false }))
    } finally {
      if (manual) setRefreshing(false)
    }
  }, [])

  const openCreate = () => {
    setEditTarget(null)
    setSetOpen(true)
  }

  const openEdit = (secret: SecretMetadata) => {
    setEditTarget(secret)
    setSetOpen(true)
  }

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex items-baseline justify-between">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">Secrets</h1>
          <p className="text-meta text-muted-foreground">
            Vault-backed secrets for this fork. Values are stored encrypted and only leave the
            runtime through an explicit reveal — reference them from agents and modules as{" "}
            <code>secret:&lt;key&gt;</code>.
          </p>
        </div>
        <div className="flex items-center gap-stack text-meta text-muted-foreground">
          <span className="tabular-nums">updated {ageLabel(snapshot.fetchedAt)}</span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => refresh(true)}
            disabled={refreshing}
            className="h-7 gap-inline text-meta"
          >
            <RefreshCw className={`size-3.5 ${refreshing ? "animate-spin" : ""}`} aria-hidden />
            Refresh
          </Button>
          <Button type="button" size="sm" onClick={openCreate} className="h-7 gap-inline text-meta">
            <Plus className="size-3.5" aria-hidden />
            New secret
          </Button>
        </div>
      </header>

      <SecretsTable
        secrets={snapshot.secrets}
        healthy={snapshot.healthy}
        onRevealed={setRevealed}
        onEdit={openEdit}
        onRotate={setRotateTarget}
        onMutated={() => refresh()}
      />

      <SetSecretDialog
        open={setOpen}
        secret={editTarget}
        onClose={() => setSetOpen(false)}
        onSaved={() => refresh()}
      />
      <RotateSecretDialog
        secret={rotateTarget}
        onClose={() => setRotateTarget(null)}
        onRotated={() => refresh()}
      />
      <RevealSecretDialog revealed={revealed} onClose={() => setRevealed(null)} />
    </div>
  )
}

function ageLabel(fetchedAt: string): string {
  const ageSec = Math.max(0, Math.round((Date.now() - Date.parse(fetchedAt)) / 1000))
  return ageSec < 5 ? "now" : `${ageSec}s ago`
}
