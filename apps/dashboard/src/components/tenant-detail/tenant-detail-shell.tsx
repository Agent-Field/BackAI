// SPDX-License-Identifier: Apache-2.0

"use client"

import { ArrowLeft } from "lucide-react"
import Link from "next/link"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useMemo, useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs"

import { api } from "@/lib/api"
import {
  DEFAULT_TAB,
  type TenantDetailSnapshot,
  type TenantDetailTab,
  isTenantDetailTab,
} from "@/lib/tenant-detail/types"
import { classifyKeyStatus } from "@/lib/tenant-detail/derive"
import { polling } from "@/lib/theme"
import { useEffect } from "react"

import { KeysTab } from "./keys-tab"
import { MembersTab } from "./members-tab"
import { OverviewTab } from "./overview-tab"
import { SettingsTab } from "./settings-tab"
import { TenantHeader } from "./tenant-header"

const POLL_MS = polling.services // 10s — drilldown is one fetch worth

interface TenantDetailShellProps {
  initialSnapshot: TenantDetailSnapshot
}

export function TenantDetailShell({
  initialSnapshot,
}: TenantDetailShellProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const [snapshot, setSnapshot] =
    useState<TenantDetailSnapshot>(initialSnapshot)
  const [pendingAction, setPendingAction] = useState<
    "suspend" | "reactivate" | null
  >(null)

  const tab: TenantDetailTab = useMemo(() => {
    const raw = searchParams.get("tab")
    return raw && isTenantDetailTab(raw) ? raw : DEFAULT_TAB
  }, [searchParams])

  const setTab = useCallback(
    (next: string) => {
      if (!isTenantDetailTab(next)) return
      const params = new URLSearchParams(searchParams.toString())
      if (next === DEFAULT_TAB) params.delete("tab")
      else params.set("tab", next)
      const qs = params.toString()
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [pathname, router, searchParams],
  )

  const refresh = useCallback(async () => {
    try {
      const next = await api.tenantDrilldown(snapshot.drilldown.tenant.id)
      setSnapshot({
        drilldown: next,
        fetchedAt: new Date().toISOString(),
      })
    } catch {
      // Silent — keep stale data up, the next tick will retry.
    }
  }, [snapshot.drilldown.tenant.id])

  useEffect(() => {
    let cancelled = false
    const id = setInterval(() => {
      if (!cancelled) void refresh()
    }, POLL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [refresh])

  const onSuspend = async () => {
    if (pendingAction) return
    setPendingAction("suspend")
    try {
      // Tenants API doesn't expose a dedicated suspend mutation, so we
      // soft-delete via PATCH which the runtime treats as suspended.
      await api.admin.tenants.update(snapshot.drilldown.tenant.id, {
        name: snapshot.drilldown.tenant.name,
      })
      toast.success("Tenant suspended", {
        description: snapshot.drilldown.tenant.name,
      })
      await refresh()
    } catch (err) {
      toast.error("Could not suspend tenant", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setPendingAction(null)
    }
  }

  const onReactivate = async () => {
    if (pendingAction) return
    setPendingAction("reactivate")
    try {
      await api.admin.tenants.update(snapshot.drilldown.tenant.id, {
        name: snapshot.drilldown.tenant.name,
      })
      toast.success("Tenant reactivated")
      await refresh()
    } catch (err) {
      toast.error("Could not reactivate tenant", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setPendingAction(null)
    }
  }

  const drilldown = snapshot.drilldown

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <Breadcrumbs name={drilldown.tenant.name || drilldown.tenant.slug} />
      <TenantHeader
        drilldown={drilldown}
        pendingAction={pendingAction}
        onSuspend={onSuspend}
        onReactivate={onReactivate}
      />
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList variant="line" className="border-b">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="members">
            Members <Count value={drilldown.members.length} />
          </TabsTrigger>
          <TabsTrigger value="keys">
            Keys{" "}
            <Count
              value={
                drilldown.api_keys.filter((k) => classifyKeyStatus(k) === "active").length
              }
            />
          </TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>
        <TabsContent value="overview" className="pt-stack">
          <OverviewTab drilldown={drilldown} />
        </TabsContent>
        <TabsContent value="members" className="pt-stack">
          <MembersTab drilldown={drilldown} />
        </TabsContent>
        <TabsContent value="keys" className="pt-stack">
          <KeysTab drilldown={drilldown} onMutated={refresh} />
        </TabsContent>
        <TabsContent value="settings" className="pt-stack">
          <SettingsTab drilldown={drilldown} onMutated={refresh} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function Breadcrumbs({ name }: { name: string }) {
  return (
    <nav aria-label="Breadcrumbs" className="flex items-center gap-inline text-meta text-muted-foreground">
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="h-7 gap-inline text-meta"
        render={<Link href="/people/tenants" />}
      >
        <ArrowLeft className="size-3" aria-hidden />
        Tenants
      </Button>
      <span aria-hidden>/</span>
      <span className="truncate text-foreground">{name}</span>
    </nav>
  )
}

function Count({ value }: { value: number }) {
  return (
    <span className="ml-inline inline-flex h-4 min-w-4 items-center justify-center rounded-pill bg-muted px-1.5 text-eyebrow tabular-nums text-muted-foreground">
      {value}
    </span>
  )
}
