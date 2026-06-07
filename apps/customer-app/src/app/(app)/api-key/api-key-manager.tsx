// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import { Copy, KeyRound, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

type Key = {
  id: string
  prefix: string
  name: string | null
  scopes: string[]
  created_at: string
  last_used_at: string | null
  revoked_at: string | null
}

type Props = {
  initialKeys: Key[]
  tenantId: string
}

function maskPrefix(prefix: string): string {
  return `af_${prefix}_••••••••••••••••••••••••••••••••••••••••••••`
}

function fmt(iso: string | null): string {
  if (!iso) return "—"
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

export function ApiKeyManager({ initialKeys, tenantId: _tenantId }: Props) {
  const [keys, setKeys] = useState<Key[]>(initialKeys)
  const [creating, setCreating] = useState(false)
  const [issued, setIssued] = useState<{ token: string; prefix: string } | null>(
    null,
  )

  const handleIssue = async () => {
    setCreating(true)
    try {
      const res = await fetch("/api/customer/onboarding-key", {
        method: "POST",
        credentials: "include",
      })
      if (!res.ok) {
        toast.error(`Could not issue key: HTTP ${res.status}`)
        return
      }
      const data = (await res.json()) as {
        api_key_token: string
        api_key_prefix: string
      }
      setIssued({ token: data.api_key_token, prefix: data.api_key_prefix })
      // Optimistic: append to list. Server-side state will refresh on next nav.
      setKeys((prev) => [
        {
          id: crypto.randomUUID(),
          prefix: data.api_key_prefix,
          name: "Default key",
          scopes: ["llm:invoke"],
          created_at: new Date().toISOString(),
          last_used_at: null,
          revoked_at: null,
        },
        ...prev,
      ])
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setCreating(false)
    }
  }

  const handleRevoke = async (id: string) => {
    const res = await fetch(`/api/customer/keys/${id}`, {
      method: "DELETE",
      credentials: "include",
    })
    if (!res.ok) {
      toast.error(`Could not revoke: HTTP ${res.status}`)
      return
    }
    setKeys((prev) =>
      prev.map((k) =>
        k.id === id ? { ...k, revoked_at: new Date().toISOString() } : k,
      ),
    )
    toast.success("Key revoked.")
  }

  const handleCopy = async (token: string) => {
    await navigator.clipboard.writeText(token)
    toast.success("Copied.")
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex justify-end">
        <Button onClick={handleIssue} disabled={creating}>
          <Plus data-icon="inline-start" />
          {creating ? "Issuing..." : "Issue new key"}
        </Button>
      </div>

      {keys.length === 0 ? (
        <p className="text-muted-foreground py-6 text-center text-sm">
          No keys yet. Issue one to get started.
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Prefix</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Scopes</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>Last used</TableHead>
              <TableHead>Status</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {keys.map((k) => (
              <TableRow key={k.id}>
                <TableCell className="font-mono text-xs">
                  {maskPrefix(k.prefix)}
                </TableCell>
                <TableCell>{k.name ?? "—"}</TableCell>
                <TableCell className="text-xs">
                  {k.scopes.map((s) => (
                    <Badge key={s} variant="outline" className="mr-1 text-[10px]">
                      {s}
                    </Badge>
                  ))}
                </TableCell>
                <TableCell className="text-muted-foreground text-xs">
                  {fmt(k.created_at)}
                </TableCell>
                <TableCell className="text-muted-foreground text-xs">
                  {fmt(k.last_used_at)}
                </TableCell>
                <TableCell>
                  {k.revoked_at ? (
                    <Badge variant="outline">revoked</Badge>
                  ) : (
                    <Badge>active</Badge>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  {!k.revoked_at ? (
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => handleRevoke(k.id)}
                      aria-label="Revoke"
                    >
                      <Trash2 />
                    </Button>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <Dialog
        open={!!issued}
        onOpenChange={(open) => {
          if (!open) setIssued(null)
        }}
      >
        <DialogContent showCloseButton={false} className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <KeyRound className="size-4" />
              New API key
            </DialogTitle>
            <DialogDescription>
              Copy this now. The plaintext is never shown again.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="rounded-md border bg-muted p-3 font-mono text-xs break-all">
              {issued?.token}
            </div>
            <div className="flex gap-2">
              <Button
                variant="outline"
                className="flex-1"
                onClick={() => issued && handleCopy(issued.token)}
              >
                <Copy data-icon="inline-start" />
                Copy
              </Button>
              <Button className="flex-1" onClick={() => setIssued(null)}>
                Done
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
