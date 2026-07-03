// SPDX-License-Identifier: Apache-2.0

"use client"

import { KeyRound, RefreshCw } from "lucide-react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useState } from "react"

import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import type { IssuedAPIKey } from "@/lib/api"
import { OPERATOR_TENANT_ID } from "@/lib/keys-page/derive"
import type { KeysSnapshot } from "@/lib/keys-page/types"

import { IssueKeyDialog } from "./issue-key-dialog"
import { KeyRevealDialog } from "./key-reveal"
import { KeysTable } from "./keys-table"

// KeysShell owns:
//   - URL-persistent tenant filter (?tenant=)
//   - Client refetch when the filter changes / after mutations
//   - The issue dialog + the rotate reveal dialog (one-time values)

interface KeysShellProps {
  initialSnapshot: KeysSnapshot
}

export function KeysShell({ initialSnapshot }: KeysShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<KeysSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)
  const [issueOpen, setIssueOpen] = useState(false)
  const [rotated, setRotated] = useState<IssuedAPIKey | null>(null)

  const tenantFilter = searchParams.get("tenant") ?? ""

  const setTenantFilter = useCallback(
    (tenant: string) => {
      const params = new URLSearchParams(searchParams.toString())
      if (!tenant) params.delete("tenant")
      else params.set("tenant", tenant)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [pathname, router, searchParams],
  )

  const refresh = useCallback(
    async (tenant: string, manual = false) => {
      if (manual) setRefreshing(true)
      const fetchedAt = new Date().toISOString()
      try {
        const [keys, tenants] = await Promise.all([
          api.admin.keys.list(tenant ? { tenant } : undefined),
          api.admin.tenants.list().catch(() => null),
        ])
        setSnapshot((prev) => ({
          keys: keys.keys,
          tenants: tenants?.tenants ?? prev.tenants,
          fetchedAt,
          healthy: true,
        }))
      } catch {
        setSnapshot((prev) => ({ ...prev, fetchedAt, healthy: false }))
      } finally {
        if (manual) setRefreshing(false)
      }
    },
    [],
  )

  useEffect(() => {
    void refresh(tenantFilter)
  }, [tenantFilter, refresh])

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex items-baseline justify-between">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">
            API keys
          </h1>
          <p className="text-meta text-muted-foreground">
            Every key on this fork. Keys on the zero-uuid tenant with an
            operator scope work as operator keys for the af-stack CLI.
          </p>
        </div>
        <div className="flex items-center gap-stack text-meta text-muted-foreground">
          <span className="tabular-nums">
            updated {ageLabel(snapshot.fetchedAt)}
          </span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => refresh(tenantFilter, true)}
            disabled={refreshing}
            className="h-7 gap-inline text-meta"
          >
            <RefreshCw
              className={`size-3.5 ${refreshing ? "animate-spin" : ""}`}
              aria-hidden
            />
            Refresh
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={() => setIssueOpen(true)}
            className="h-7 gap-inline text-meta"
          >
            <KeyRound className="size-3.5" aria-hidden />
            Issue key
          </Button>
        </div>
      </header>

      <div className="flex flex-wrap items-center gap-stack rounded-md border bg-card/95 px-row-x py-row-y">
        <label className="flex items-center gap-inline text-meta text-muted-foreground">
          <span className="text-eyebrow uppercase tracking-wide">Tenant</span>
          <select
            value={tenantFilter}
            onChange={(e) => setTenantFilter(e.target.value)}
            className="h-7 rounded-lg border border-input bg-transparent px-2 text-meta text-foreground outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
          >
            <option value="">All tenants</option>
            <option value={OPERATOR_TENANT_ID}>operator (zero-uuid)</option>
            {snapshot.tenants
              .filter((t) => t.id !== OPERATOR_TENANT_ID)
              .map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name || t.slug}
                </option>
              ))}
          </select>
        </label>
      </div>

      <KeysTable
        keys={snapshot.keys}
        tenants={snapshot.tenants}
        healthy={snapshot.healthy}
        onMutated={() => refresh(tenantFilter)}
        onRotated={setRotated}
      />

      <IssueKeyDialog
        open={issueOpen}
        tenants={snapshot.tenants}
        defaultTenantId={tenantFilter || undefined}
        onClose={() => setIssueOpen(false)}
        onIssued={() => refresh(tenantFilter)}
      />
      <KeyRevealDialog
        issued={rotated}
        title="Key rotated"
        onClose={() => setRotated(null)}
      />
    </div>
  )
}

function ageLabel(fetchedAt: string): string {
  const ageSec = Math.max(
    0,
    Math.round((Date.now() - Date.parse(fetchedAt)) / 1000),
  )
  return ageSec < 5 ? "now" : `${ageSec}s ago`
}
