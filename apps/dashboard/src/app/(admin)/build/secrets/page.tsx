// SPDX-License-Identifier: Apache-2.0

// Secrets page — Build → Secrets. Secret keys in the vault, backed by GET
// /api/v1/secrets. Values are write-only and never listed. Server component:
// fetched once per render (force-dynamic + no-store via lib/api.ts). When the
// runtime isn't reachable we render a clean empty-state shell rather than
// crashing.

import { KeyRound, Lock } from "lucide-react"

import { api, ApiError, type SecretList } from "@/lib/api"
import { PageHeader } from "@/components/layout/page-header"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export const dynamic = "force-dynamic"

async function loadSecrets(): Promise<{
  data: SecretList
  error: string | null
}> {
  try {
    const data = await api.secrets.list()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load secrets"
    return { data: { secrets: [] }, error: message }
  }
}

export default async function Page() {
  const { data, error } = await loadSecrets()
  const secrets = data.secrets

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Secrets"
        description="Secret keys in the vault. Values are write-only and never listed. Backed by /api/v1/secrets."
      />

      {error && secrets.length === 0 ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
          <p className="text-foreground mb-1 font-medium">
            Secret vault isn&apos;t available
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            .
          </p>
        </div>
      ) : secrets.length === 0 ? (
        <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
          <KeyRound className="size-3.5" />
          No secrets stored.
        </div>
      ) : (
        <div className="bg-card rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Key
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Description
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Tenant
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Rotate after
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Updated
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {secrets.map((secret) => (
                <TableRow key={secret.key} className="hover:bg-muted/30">
                  <TableCell>
                    <code className="font-mono text-xs">{secret.key}</code>
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {secret.description ?? "—"}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {secret.tenant_id ?? "global"}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {secret.rotate_after ?? "—"}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs tabular-nums">
                    {secret.updated_at}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <p className="text-muted-foreground flex items-center gap-2 text-xs">
        <Lock className="size-3.5" />
        Secret values are revealed individually via the reveal endpoint; they
        are never returned in this list.
      </p>
    </div>
  )
}
