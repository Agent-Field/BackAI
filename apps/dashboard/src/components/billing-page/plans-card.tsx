// SPDX-License-Identifier: Apache-2.0

"use client"

import { Plus } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { api } from "@/lib/api"
import type { BillingPlan } from "@/lib/api"

import { PlanFormDialog } from "./plan-form-dialog"

// Plans catalog card. One row per plan: name/id, monthly price, Stripe
// Price binding, enforced LLM budget, entitlements as compact JSON, and
// the default badge. Edit opens the upsert dialog; "Set default" upserts
// the same plan with is_default=true; delete is rejected server-side for
// the default plan so the button is disabled there too.

interface PlansCardProps {
  plans: BillingPlan[]
  onMutated: () => Promise<void> | void
}

export function PlansCard({ plans, onMutated }: PlansCardProps) {
  const [editing, setEditing] = useState<BillingPlan | null>(null)
  const [creating, setCreating] = useState(false)
  const [busyId, setBusyId] = useState<string | null>(null)

  const setDefault = async (plan: BillingPlan) => {
    setBusyId(plan.id)
    try {
      await api.admin.billing.upsertPlan({
        id: plan.id,
        name: plan.name,
        stripe_price_id: plan.stripe_price_id,
        price_usd_month: plan.price_usd_month,
        llm_budget_usd: plan.llm_budget_usd,
        entitlements: plan.entitlements,
        is_default: true,
      })
      toast.success(`"${plan.name}" is now the default plan`)
      await onMutated()
    } catch (err) {
      toast.error("Could not set the default plan", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setBusyId(null)
    }
  }

  const remove = async (plan: BillingPlan) => {
    if (!window.confirm(`Delete plan "${plan.name}" (${plan.id})?`)) return
    setBusyId(plan.id)
    try {
      await api.admin.billing.deletePlan(plan.id)
      toast.success(`Plan "${plan.name}" deleted`)
      await onMutated()
    } catch (err) {
      toast.error("Could not delete the plan", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setBusyId(null)
    }
  }

  return (
    <ZoneCard>
      <ZoneCardHeader
        title="Plans"
        subtitle={
          plans.length === 0
            ? "no plans yet"
            : `${plans.length} plan${plans.length === 1 ? "" : "s"} in the catalog`
        }
        trailing={
          <Button
            type="button"
            size="sm"
            onClick={() => setCreating(true)}
            className="h-7 gap-inline text-meta"
          >
            <Plus className="size-3.5" aria-hidden />
            Add plan
          </Button>
        }
      />

      {plans.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No plans in the catalog. Add one — the default plan is what new
          tenants land on, and a plan&apos;s LLM budget becomes the
          tenant&apos;s enforced monthly cap the moment the plan applies.
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left">
            <thead>
              <tr className="border-b text-eyebrow uppercase tracking-wide text-muted-foreground">
                <th scope="col" className="px-row-x py-row-y font-medium">
                  Plan
                </th>
                <th scope="col" className="px-row-x py-row-y font-medium">
                  Price / mo
                </th>
                <th scope="col" className="px-row-x py-row-y font-medium">
                  Stripe price
                </th>
                <th scope="col" className="px-row-x py-row-y font-medium">
                  LLM budget
                </th>
                <th scope="col" className="px-row-x py-row-y font-medium">
                  Entitlements
                </th>
                <th scope="col" className="px-row-x py-row-y text-right font-medium">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {plans.map((plan) => (
                <tr key={plan.id} className="align-top">
                  <td className="px-row-x py-row-y">
                    <div className="flex min-w-0 flex-col gap-tile-tight">
                      <span className="flex items-center gap-tile-tight">
                        <span className="truncate text-body font-medium text-foreground">
                          {plan.name}
                        </span>
                        {plan.is_default ? (
                          <Badge variant="secondary">default</Badge>
                        ) : null}
                      </span>
                      <code className="font-mono text-meta text-muted-foreground">
                        {plan.id}
                      </code>
                    </div>
                  </td>
                  <td className="px-row-x py-row-y text-body tabular-nums text-foreground">
                    ${plan.price_usd_month.toFixed(2)}
                  </td>
                  <td className="px-row-x py-row-y">
                    {plan.stripe_price_id ? (
                      <code className="font-mono text-meta text-muted-foreground">
                        {plan.stripe_price_id}
                      </code>
                    ) : (
                      <span className="text-meta text-muted-foreground">—</span>
                    )}
                  </td>
                  <td className="px-row-x py-row-y text-body tabular-nums text-foreground">
                    {plan.llm_budget_usd === null
                      ? "unlimited"
                      : `$${plan.llm_budget_usd.toFixed(2)}`}
                  </td>
                  <td className="max-w-56 px-row-x py-row-y">
                    <EntitlementsPreview entitlements={plan.entitlements} />
                  </td>
                  <td className="px-row-x py-row-y">
                    <div className="flex items-center justify-end gap-inline">
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        className="h-7 text-meta"
                        disabled={busyId !== null}
                        onClick={() => setEditing(plan)}
                      >
                        Edit
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        className="h-7 text-meta"
                        disabled={busyId !== null || plan.is_default}
                        title={
                          plan.is_default
                            ? "Already the default plan"
                            : "Make this the default plan for new tenants"
                        }
                        onClick={() => setDefault(plan)}
                      >
                        {busyId === plan.id ? "Working…" : "Set default"}
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        className="h-7 text-meta text-destructive hover:text-destructive"
                        disabled={busyId !== null || plan.is_default}
                        title={
                          plan.is_default
                            ? "The default plan cannot be deleted"
                            : "Delete this plan"
                        }
                        onClick={() => remove(plan)}
                      >
                        Delete
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <PlanFormDialog
        open={creating || editing !== null}
        plan={editing}
        onClose={() => {
          setCreating(false)
          setEditing(null)
        }}
        onSaved={onMutated}
      />
    </ZoneCard>
  )
}

function EntitlementsPreview({
  entitlements,
}: {
  entitlements: Record<string, unknown>
}) {
  const compact = JSON.stringify(entitlements)
  if (compact === "{}") {
    return <span className="text-meta text-muted-foreground">—</span>
  }
  return (
    <code
      title={JSON.stringify(entitlements, null, 2)}
      className="block truncate rounded bg-muted px-1.5 py-0.5 font-mono text-meta text-muted-foreground"
    >
      {compact}
    </code>
  )
}
