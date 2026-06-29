// SPDX-License-Identifier: Apache-2.0

"use client"

import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { applyFilters, groupBySeverity, mergeInboxItems } from "@/lib/inbox/derive"
import type {
  InboxFilters,
  InboxItem,
  InboxKind,
  InboxSeverity,
  InboxSnapshot,
} from "@/lib/inbox/types"
import { DEFAULT_FILTERS } from "@/lib/inbox/types"
import { polling } from "@/lib/theme"

import { AllClear } from "./all-clear"
import { ApprovalDrawer } from "./approval-drawer"
import { FilterChips } from "./filter-chips"
import { InboxBanner } from "./inbox-banner"
import { ItemGroup } from "./item-group"

// Top-level Inbox surface. Owns:
//   - URL-persistent filter chip state (?severity=, ?kind=)
//   - URL-persistent drawer state (?item=approval:<id>)
//   - 30-second polling tick (theme.polling.inbox)
//   - Optimistic refresh after a decide mutation
//
// The page brief (development/ux/pages/inbox.md) calls for an affirmative
// "All clear" empty state, severity-tiered ordering, and a right-side
// drawer for approval detail. All three are implemented here.

interface InboxShellProps {
  initialSnapshot: InboxSnapshot
}

function parseSeverity(raw: string | null): InboxFilters["severity"] {
  if (raw === "critical" || raw === "warning" || raw === "info") return raw
  return "all"
}

function parseKind(raw: string | null): InboxFilters["kind"] {
  if (raw === "approval" || raw === "alert") return raw
  return "all"
}

export function InboxShell({ initialSnapshot }: InboxShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] = useState<InboxSnapshot>(initialSnapshot)

  const filters = useMemo<InboxFilters>(
    () => ({
      severity: parseSeverity(searchParams.get("severity")),
      kind: parseKind(searchParams.get("kind")),
    }),
    [searchParams],
  )

  const selectedItemId = searchParams.get("item")

  // Filter chips drive URL state. The shell re-reads from URL on every
  // render so back/forward + shared links work.
  const setFilters = useCallback(
    (next: Partial<InboxFilters>) => {
      const params = new URLSearchParams(searchParams.toString())
      const final: InboxFilters = { ...filters, ...next }
      if (final.severity === DEFAULT_FILTERS.severity) params.delete("severity")
      else params.set("severity", final.severity)
      if (final.kind === DEFAULT_FILTERS.kind) params.delete("kind")
      else params.set("kind", final.kind)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [filters, pathname, router, searchParams],
  )

  const openDrawerFor = useCallback(
    (id: string | null) => {
      const params = new URLSearchParams(searchParams.toString())
      if (id === null) params.delete("item")
      else params.set("item", id)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [pathname, router, searchParams],
  )

  // Polling tick. Re-fetch both sources every 30s and re-merge. We don't
  // use SWR/react-query here so the cadence stays explicit and matches
  // the page brief.
  const refresh = useCallback(async () => {
    const fetchedAt = new Date().toISOString()
    const [approvalsResult, homeResult] = await Promise.allSettled([
      api.approvals.list({ status: "pending", limit: 100 }),
      api.home(),
    ])
    const approvals =
      approvalsResult.status === "fulfilled"
        ? approvalsResult.value.approvals
        : []
    const alerts =
      homeResult.status === "fulfilled" ? homeResult.value.alerts : []
    setSnapshot({
      items: mergeInboxItems(approvals, alerts, fetchedAt),
      approvalsHealthy: approvalsResult.status === "fulfilled",
      alertsHealthy: homeResult.status === "fulfilled",
      fetchedAt,
    })
  }, [])

  useEffect(() => {
    let cancelled = false
    const id = setInterval(() => {
      if (!cancelled) void refresh()
    }, polling.inbox)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [refresh])

  const filtered = useMemo(
    () => applyFilters(snapshot.items, filters),
    [snapshot.items, filters],
  )
  const grouped = useMemo(() => groupBySeverity(filtered), [filtered])

  const selectedItem = useMemo(
    () => snapshot.items.find((i) => i.id === selectedItemId) ?? null,
    [snapshot.items, selectedItemId],
  )

  const handleRowClick = useCallback(
    (item: InboxItem) => {
      // System alerts have no detail surface in v1 — they vanish only
      // when the underlying condition resolves (Gap 10). Clicks on
      // alert rows are no-ops aside from the drill button rendered in
      // the row itself.
      if (item.kind === "approval") openDrawerFor(item.id)
    },
    [openDrawerFor],
  )

  const handleDecide = useCallback(
    async (
      itemId: string,
      decision: "approved" | "denied" | "cancelled",
      note?: string,
    ) => {
      const item = snapshot.items.find((i) => i.id === itemId)
      if (!item || item.kind !== "approval") return
      try {
        await api.approvals.decide(item.sourceId, {
          status: decision,
          decision_note: note,
        })
        toast.success(`Approval ${decision}`, {
          description: item.title,
        })
        openDrawerFor(null)
        await refresh()
      } catch (e) {
        toast.error("Could not record decision", {
          description: e instanceof Error ? e.message : String(e),
        })
      }
    },
    [snapshot.items, openDrawerFor, refresh],
  )

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <InboxHeader
        total={snapshot.items.length}
        shown={filtered.length}
        fetchedAt={snapshot.fetchedAt}
      />

      <InboxBanner
        approvalsHealthy={snapshot.approvalsHealthy}
        alertsHealthy={snapshot.alertsHealthy}
        onRetry={refresh}
      />

      <FilterChips
        filters={filters}
        items={snapshot.items}
        onChange={setFilters}
      />

      {filtered.length === 0 ? (
        snapshot.items.length === 0 ? (
          <AllClear />
        ) : (
          <FilteredAway onClear={() => setFilters(DEFAULT_FILTERS)} />
        )
      ) : (
        <div className="flex flex-col gap-section">
          {grouped.map((group) => (
            <ItemGroup
              key={group.severity}
              severity={group.severity as InboxSeverity}
              items={group.items}
              onRowClick={handleRowClick}
              selectedId={selectedItemId}
            />
          ))}
        </div>
      )}

      <ApprovalDrawer
        item={selectedItem?.kind === "approval" ? selectedItem : null}
        onClose={() => openDrawerFor(null)}
        onDecide={handleDecide}
      />
    </div>
  )
}

function InboxHeader({
  total,
  shown,
  fetchedAt,
}: {
  total: number
  shown: number
  fetchedAt: string
}) {
  const ageSec = Math.max(0, Math.round((Date.now() - Date.parse(fetchedAt)) / 1000))
  const ageLabel = ageSec < 5 ? "now" : `${ageSec}s ago`
  return (
    <header className="flex items-baseline justify-between">
      <div className="flex flex-col gap-tile-tight">
        <h1 className="text-xl font-semibold tracking-tight text-foreground">
          Inbox
        </h1>
        <p className="text-meta text-muted-foreground">
          Things needing your attention right now.
        </p>
      </div>
      <div className="flex items-center gap-stack text-meta text-muted-foreground">
        <span className="tabular-nums">
          {shown === total ? `${total} items` : `${shown} of ${total}`}
        </span>
        <span aria-hidden>·</span>
        <span className="tabular-nums">updated {ageLabel}</span>
      </div>
    </header>
  )
}

// Filters reduced the visible list to nothing, but the snapshot has items.
// Distinct from AllClear: we want the operator to know the filters are
// hiding work, not that the platform is calm.
function FilteredAway({ onClear }: { onClear: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center gap-tile rounded-md border bg-card px-row-x py-12 text-center">
      <p className="text-body text-foreground">
        No items match the current filters.
      </p>
      <button
        type="button"
        onClick={onClear}
        className="text-meta text-muted-foreground underline hover:no-underline"
      >
        Clear filters
      </button>
    </div>
  )
}

// InboxKind is referenced via the union type for completeness; the noop
// reference keeps tree-shaking happy when callers re-export the module.
export type { InboxKind }
