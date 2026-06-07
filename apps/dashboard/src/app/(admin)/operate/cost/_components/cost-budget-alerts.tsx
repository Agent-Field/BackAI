// Budget alerts.
//
// Renders one shadcn Alert per budget that has crossed its threshold,
// destructive when fully over-spent, default otherwise. The list is
// pre-filtered by the server component; this is presentation only.
// Renders nothing when there are no triggered alerts so the page stays
// quiet on the happy path.

import { AlertTriangle, ShieldAlert } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import type { Budget, Tenant } from "@/lib/api"

import { formatCurrency, formatCurrencyCompact } from "./format"

type CostBudgetAlertsProps = {
  budgets: Budget[]
  tenants: Tenant[]
}

type TriggeredAlert = {
  budget: Budget
  tenantName: string
  pctUsed: number
  isOver: boolean
}

function buildAlerts(
  budgets: Budget[],
  tenants: Tenant[],
): TriggeredAlert[] {
  const byId = new Map(tenants.map((t) => [t.id, t]))
  const triggered: TriggeredAlert[] = []
  for (const b of budgets) {
    if (b.monthly_usd <= 0) continue
    const pctUsed = (b.spent_this_period_usd / b.monthly_usd) * 100
    if (pctUsed < b.alert_threshold_pct) continue
    const tenant = byId.get(b.tenant_id)
    triggered.push({
      budget: b,
      tenantName: tenant?.name ?? tenant?.slug ?? b.tenant_id,
      pctUsed,
      isOver: pctUsed >= 100,
    })
  }
  // Worst offenders first.
  return triggered.sort((a, b) => b.pctUsed - a.pctUsed)
}

export function CostBudgetAlerts({ budgets, tenants }: CostBudgetAlertsProps) {
  const alerts = buildAlerts(budgets, tenants)
  if (alerts.length === 0) return null

  return (
    <div className="flex flex-col gap-2">
      {alerts.map((a) => {
        const Icon = a.isOver ? ShieldAlert : AlertTriangle
        const overBy = a.budget.spent_this_period_usd - a.budget.monthly_usd
        return (
          <Alert
            key={a.budget.tenant_id}
            variant={a.isOver ? "destructive" : "default"}
          >
            <Icon />
            <AlertTitle>
              {a.tenantName}: {a.pctUsed.toFixed(0)}% of monthly budget
              {a.isOver
                ? ` — over by ${formatCurrency(overBy)}`
                : ` — past ${a.budget.alert_threshold_pct}% threshold`}
            </AlertTitle>
            <AlertDescription>
              {formatCurrency(a.budget.spent_this_period_usd)} spent of{" "}
              {formatCurrencyCompact(a.budget.monthly_usd)} this period.
              {a.budget.remaining_usd > 0
                ? ` ${formatCurrency(a.budget.remaining_usd)} remaining.`
                : ""}
            </AlertDescription>
          </Alert>
        )
      })}
    </div>
  )
}
