// SPDX-License-Identifier: Apache-2.0

"use client"

import { FlaskConical, RefreshCw } from "lucide-react"
import { useCallback, useState } from "react"

import { Button } from "@/components/ui/button"

import { api } from "@/lib/api"
import type { BillingSnapshot } from "@/lib/billing-page/data"

import { PlansCard } from "./plans-card"
import { StripeConnectionCard } from "./stripe-connection-card"

// BillingShell owns:
//   - The snapshot (settings + plans) and client refetch after mutations
//   - The stub-mode banner
// The two cards own their own forms and call back via onMutated /
// onSettingsSaved so the whole page stays consistent after any write.

interface BillingShellProps {
  initialSnapshot: BillingSnapshot
}

export function BillingShell({ initialSnapshot }: BillingShellProps) {
  const [snapshot, setSnapshot] = useState<BillingSnapshot>(initialSnapshot)
  const [refreshing, setRefreshing] = useState(false)

  const refresh = useCallback(async (manual = false) => {
    if (manual) setRefreshing(true)
    const fetchedAt = new Date().toISOString()
    try {
      const [settings, plans] = await Promise.all([
        api.admin.billing.settings().catch(() => null),
        api.billing.plans().catch(() => null),
      ])
      setSnapshot((prev) => ({
        settings: settings ?? prev.settings,
        plans: plans?.plans ?? prev.plans,
        fetchedAt,
        ok: settings !== null,
      }))
    } finally {
      if (manual) setRefreshing(false)
    }
  }, [])

  const { settings, plans } = snapshot
  const stubMode = settings !== null && settings.mode === "stub"

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex items-baseline justify-between">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">
            Billing
          </h1>
          <p className="max-w-prose text-meta text-muted-foreground">
            Turnkey billing setup. Paste Stripe keys (stored encrypted,
            hot-swapped into the live client — no restart), bind catalog plans
            to Stripe Prices, and checkout → webhook → plan applied → enforced
            LLM budget closes automatically.
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
            onClick={() => refresh(true)}
            disabled={refreshing}
            className="h-7 gap-inline text-meta"
          >
            <RefreshCw
              className={`size-3.5 ${refreshing ? "animate-spin" : ""}`}
              aria-hidden
            />
            Refresh
          </Button>
        </div>
      </header>

      {stubMode ? (
        <div
          role="status"
          className="flex items-start gap-stack rounded-md border border-amber-500/40 bg-amber-500/10 px-row-x py-row-y"
        >
          <FlaskConical
            className="mt-0.5 size-icon-inline shrink-0 text-amber-600 dark:text-amber-400"
            aria-hidden
          />
          <div className="flex flex-col gap-tile-tight">
            <span className="text-body font-medium text-foreground">
              No Stripe keys — checkout runs in dev mode (plans apply
              instantly, nothing is charged).
            </span>
            <span className="text-meta text-muted-foreground">
              Paste a Stripe secret key below to switch to real checkout.
            </span>
          </div>
        </div>
      ) : null}

      {settings === null ? (
        <div className="rounded-md border bg-card/40 px-row-x py-tile text-meta text-muted-foreground">
          Could not reach the runtime billing settings endpoint. Start the
          backend with af-stack dev, then refresh.
        </div>
      ) : (
        <StripeConnectionCard
          settings={settings}
          onSaved={() => refresh()}
        />
      )}

      <PlansCard
        plans={plans}
        realMode={settings !== null && settings.mode === "real"}
        onMutated={() => refresh()}
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
