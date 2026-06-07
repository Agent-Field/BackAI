// SPDX-License-Identifier: Apache-2.0

"use client"

// TenantDrilldownView — Phase 12.1 "everything about this tenant" page.
//
// Section-per-Card layout: header KPIs, usage sparklines, members,
// API keys, recent runs, recent webhooks, optional billing snapshot,
// recent audit. The drilldown payload comes from the server component
// pre-fetched at request time; this component is pure presentation
// with one optional client-side mutation (the "Open Stripe Portal"
// button calls api.billing.portalLink and redirects).

import * as React from "react"
import Link from "next/link"
import { Area, AreaChart, ResponsiveContainer, Tooltip } from "recharts"
import {
  Activity,
  ArrowDownToLine,
  ArrowLeft,
  ArrowUpFromLine,
  CircleDollarSign,
  CreditCard,
  ExternalLink,
  KeyRound,
  PlayCircle,
  ServerCrash,
  Users,
  Webhook,
} from "lucide-react"
import { toast } from "sonner"

import {
  api,
  ApiError,
  type AuditEntry,
  type TenantDrilldown,
} from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { cn } from "@/lib/utils"

import { formatRelative } from "../../../../operate/_lib/format"

// ─── Formatters ───────────────────────────────────────────────────────────

function formatCompact(n: number): string {
  return new Intl.NumberFormat("en-US", {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(n)
}

function formatCurrency(usd: number): string {
  const abs = Math.abs(usd)
  const digits = abs === 0 ? 2 : abs < 0.0001 ? 6 : abs < 0.01 ? 4 : 2
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(usd)
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1,
  )
  const value = bytes / Math.pow(1024, i)
  return `${value.toFixed(value >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  const mins = Math.floor(ms / 60_000)
  const secs = Math.round((ms % 60_000) / 1000)
  return `${mins}m ${secs}s`
}

function planBadgeVariant(
  plan: string,
): "default" | "secondary" | "outline" {
  switch (plan) {
    case "enterprise":
      return "default"
    case "pro":
      return "secondary"
    default:
      return "outline"
  }
}

function runStatusBadgeVariant(
  status: string,
): "default" | "secondary" | "destructive" | "outline" {
  switch (status) {
    case "succeeded":
      return "secondary"
    case "failed":
      return "destructive"
    default:
      return "outline"
  }
}

function webhookStatusBadgeVariant(
  status: string,
): "default" | "secondary" | "destructive" | "outline" {
  switch (status) {
    case "delivered":
    case "succeeded":
      return "secondary"
    case "failed":
    case "dropped_invalid_signature":
    case "dropped_duplicate":
      return "destructive"
    case "queued":
    case "delivering":
      return "default"
    default:
      return "outline"
  }
}

// ─── Top-level component ──────────────────────────────────────────────────

type Props = {
  drilldown: TenantDrilldown | null
  audit: AuditEntry[]
  loadError: string | null
  tenantId: string
}

export function TenantDrilldownView({
  drilldown,
  audit,
  loadError,
  tenantId,
}: Props) {
  if (loadError || !drilldown) {
    return (
      <div className="flex flex-col gap-6">
        <BackLink />
        <Empty className="border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ServerCrash />
            </EmptyMedia>
            <EmptyTitle>Could not load tenant</EmptyTitle>
            <EmptyDescription>
              {loadError ? (
                <>
                  The runtime returned: <code className="font-mono">{loadError}</code>
                </>
              ) : (
                <>
                  No drilldown payload available for tenant{" "}
                  <span className="font-mono">{tenantId}</span>.
                </>
              )}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  const { tenant, members, api_keys, usage, recent_runs, recent_webhooks, billing } =
    drilldown

  return (
    <div className="flex flex-col gap-6">
      <BackLink />

      <TenantHeader
        tenant={tenant}
        memberCount={members.length}
        requests30d={usage.requests_30d}
        cost30d={usage.cost_usd_30d}
      />

      <UsageCharts
        costSparkline={usage.cost_sparkline}
        requestSparkline={usage.request_sparkline}
        storageBytes={usage.storage_bytes}
        secretsCount={usage.secrets_count}
      />

      <MembersCard members={members} />

      <ApiKeysCard apiKeys={api_keys} />

      <RecentRunsCard runs={recent_runs} />

      <RecentWebhooksCard webhooks={recent_webhooks} />

      {billing ? (
        <BillingCard billing={billing} tenantId={tenant.id} />
      ) : null}

      <RecentAuditCard audit={audit} />
    </div>
  )
}

function BackLink() {
  return (
    <Link
      href="/customers/tenants"
      className={cn(
        "text-muted-foreground hover:text-foreground",
        "flex items-center gap-1.5 text-xs font-medium",
      )}
    >
      <ArrowLeft className="size-3.5" />
      Back to tenants
    </Link>
  )
}

// ─── Header (slug + plan + KPIs strip) ────────────────────────────────────

function TenantHeader({
  tenant,
  memberCount,
  requests30d,
  cost30d,
}: {
  tenant: TenantDrilldown["tenant"]
  memberCount: number
  requests30d: number
  cost30d: number
}) {
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex flex-col gap-1">
            <CardTitle className="flex items-center gap-2 text-2xl">
              {tenant.name}
              <Badge variant={planBadgeVariant(tenant.plan)}>{tenant.plan}</Badge>
            </CardTitle>
            <CardDescription className="font-mono text-xs">
              {tenant.slug} · {tenant.id}
            </CardDescription>
          </div>
          <div className="grid grid-cols-3 gap-3 text-right">
            <HeaderStat
              icon={<Users className="size-3.5" />}
              label="Members"
              value={memberCount.toLocaleString()}
            />
            <HeaderStat
              icon={<Activity className="size-3.5" />}
              label="Requests 30d"
              value={formatCompact(requests30d)}
            />
            <HeaderStat
              icon={<CircleDollarSign className="size-3.5" />}
              label="Cost 30d"
              value={formatCurrency(cost30d)}
            />
          </div>
        </div>
      </CardHeader>
    </Card>
  )
}

function HeaderStat({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode
  label: string
  value: string
}) {
  return (
    <div className="flex flex-col items-end gap-0.5">
      <span className="text-muted-foreground flex items-center gap-1 text-[10px] font-medium uppercase tracking-wide">
        {icon}
        {label}
      </span>
      <span className="font-mono text-lg tabular-nums">{value}</span>
    </div>
  )
}

// ─── Usage charts (24h cost + requests inline) ────────────────────────────

function UsageCharts({
  costSparkline,
  requestSparkline,
  storageBytes,
  secretsCount,
}: {
  costSparkline: number[]
  requestSparkline: number[]
  storageBytes: number
  secretsCount: number
}) {
  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <SparkCard
        title="Cost (last 24h)"
        description="Hourly buckets in USD."
        icon={<CircleDollarSign className="size-4" />}
        sparkline={costSparkline}
        formatValue={formatCurrency}
        secondaryLabel="Secrets"
        secondaryValue={secretsCount.toLocaleString()}
      />
      <SparkCard
        title="Requests (last 24h)"
        description="Hourly buckets, gateway requests only."
        icon={<Activity className="size-4" />}
        sparkline={requestSparkline}
        formatValue={(n) => formatCompact(n)}
        secondaryLabel="Storage"
        secondaryValue={formatBytes(storageBytes)}
      />
    </div>
  )
}

function SparkCard({
  title,
  description,
  icon,
  sparkline,
  formatValue,
  secondaryLabel,
  secondaryValue,
}: {
  title: string
  description: string
  icon: React.ReactNode
  sparkline: number[]
  formatValue: (n: number) => string
  secondaryLabel: string
  secondaryValue: string
}) {
  const data = sparkline.map((v, i) => ({ i, value: v }))
  const total = sparkline.reduce((a, b) => a + b, 0)
  const peak = sparkline.length === 0 ? 0 : Math.max(...sparkline)
  const hasData = total > 0

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-2">
          <CardTitle className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
            {icon}
            {title}
          </CardTitle>
          <Badge variant="outline" className="font-mono">
            {secondaryLabel}: {secondaryValue}
          </Badge>
        </div>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="flex items-baseline gap-3">
          <span className="text-3xl font-semibold tabular-nums">
            {formatValue(total)}
          </span>
          <span className="text-muted-foreground text-xs">
            Peak hour: {formatValue(peak)}
          </span>
        </div>
        <div className="mt-3 h-20 w-full">
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart
              data={
                data.length > 0 ? data : [{ i: 0, value: 0 }, { i: 1, value: 0 }]
              }
              margin={{ top: 4, right: 0, left: 0, bottom: 0 }}
            >
              <defs>
                <linearGradient
                  id={`spark-${title.replace(/\s+/g, "-")}`}
                  x1="0"
                  y1="0"
                  x2="0"
                  y2="1"
                >
                  <stop offset="0%" stopColor="var(--chart-1)" stopOpacity={0.35} />
                  <stop offset="100%" stopColor="var(--chart-1)" stopOpacity={0} />
                </linearGradient>
              </defs>
              <Area
                type="monotone"
                dataKey="value"
                stroke="var(--chart-1)"
                strokeWidth={1.5}
                fill={`url(#spark-${title.replace(/\s+/g, "-")})`}
                isAnimationActive={false}
              />
              {hasData ? (
                <Tooltip
                  cursor={{ stroke: "var(--border)" }}
                  formatter={(value) => [
                    formatValue(typeof value === "number" ? value : 0),
                    "Value",
                  ]}
                  labelFormatter={(label) => {
                    const idx = typeof label === "number" ? label : 0
                    const hoursAgo = 23 - idx
                    return hoursAgo === 0
                      ? "Current hour"
                      : `${hoursAgo}h ago`
                  }}
                  contentStyle={{
                    background: "var(--popover)",
                    color: "var(--popover-foreground)",
                    border: "1px solid var(--border)",
                    borderRadius: "6px",
                    fontSize: "11px",
                  }}
                />
              ) : null}
            </AreaChart>
          </ResponsiveContainer>
        </div>
        {!hasData ? (
          <p className="text-muted-foreground mt-2 text-xs">
            No traffic in the last 24 hours.
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}

// ─── Members table ────────────────────────────────────────────────────────

function MembersCard({ members }: { members: TenantDrilldown["members"] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-sm font-medium">
          <Users className="size-4 text-muted-foreground" />
          Members ({members.length})
        </CardTitle>
        <CardDescription>
          Operators with access to this tenant. Last active is computed from
          API-key usage.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {members.length === 0 ? (
          <EmptyInline message="No members yet." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  User
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Role
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Last active
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {members.map((m) => (
                <TableRow key={m.user.id} className="hover:bg-muted/30">
                  <TableCell>
                    <div className="flex flex-col">
                      <span className="text-sm font-medium">
                        {m.user.name ?? m.user.email}
                      </span>
                      <span className="text-muted-foreground font-mono text-xs">
                        {m.user.email}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">{m.role}</Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground font-mono text-xs">
                    {formatRelative(m.last_active_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

// ─── API keys table ───────────────────────────────────────────────────────

function ApiKeysCard({
  apiKeys,
}: {
  apiKeys: TenantDrilldown["api_keys"]
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-sm font-medium">
          <KeyRound className="size-4 text-muted-foreground" />
          API keys ({apiKeys.length})
        </CardTitle>
        <CardDescription>
          Tenant-scoped credentials. Plaintext values are revealed once at
          issue time and never re-shown.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {apiKeys.length === 0 ? (
          <EmptyInline message="No keys issued for this tenant." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Prefix
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Name
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Scopes
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Last used
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Status
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {apiKeys.map((k) => (
                <TableRow key={k.id} className="hover:bg-muted/30">
                  <TableCell className="font-mono text-xs">{k.prefix}…</TableCell>
                  <TableCell className="text-sm">{k.name ?? "Unnamed"}</TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {k.scopes.length === 0 ? (
                        <span className="text-muted-foreground text-xs">—</span>
                      ) : (
                        k.scopes.map((s) => (
                          <Badge
                            key={s}
                            variant="outline"
                            className="font-mono text-[10px]"
                          >
                            {s}
                          </Badge>
                        ))
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="text-muted-foreground font-mono text-xs">
                    {formatRelative(k.last_used_at)}
                  </TableCell>
                  <TableCell>
                    <Badge variant={k.revoked_at ? "destructive" : "secondary"}>
                      {k.revoked_at ? "Revoked" : "Active"}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

// ─── Recent runs table ────────────────────────────────────────────────────

function RecentRunsCard({
  runs,
}: {
  runs: TenantDrilldown["recent_runs"]
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-sm font-medium">
          <PlayCircle className="size-4 text-muted-foreground" />
          Recent runs ({runs.length})
        </CardTitle>
        <CardDescription>
          The last ten agent invocations for this tenant.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {runs.length === 0 ? (
          <EmptyInline message="No agent runs recorded yet." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Agent
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Status
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Started
                </TableHead>
                <TableHead className="text-muted-foreground text-right text-xs font-medium uppercase tracking-wide">
                  Duration
                </TableHead>
                <TableHead className="text-muted-foreground text-right text-xs font-medium uppercase tracking-wide">
                  Cost
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map((r) => (
                <TableRow key={r.id} className="hover:bg-muted/30">
                  <TableCell className="font-mono text-xs">{r.agent}</TableCell>
                  <TableCell>
                    <Badge variant={runStatusBadgeVariant(r.status)}>
                      {r.status}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground font-mono text-xs">
                    {formatRelative(r.started_at)}
                  </TableCell>
                  <TableCell className="text-right font-mono tabular-nums text-xs">
                    {formatDuration(r.duration_ms)}
                  </TableCell>
                  <TableCell className="text-right font-mono tabular-nums text-xs">
                    {formatCurrency(r.cost_usd)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

// ─── Recent webhooks table ────────────────────────────────────────────────

function RecentWebhooksCard({
  webhooks,
}: {
  webhooks: TenantDrilldown["recent_webhooks"]
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-sm font-medium">
          <Webhook className="size-4 text-muted-foreground" />
          Recent webhooks ({webhooks.length})
        </CardTitle>
        <CardDescription>
          The last ten inbound + outbound webhook deliveries for this tenant.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {webhooks.length === 0 ? (
          <EmptyInline message="No webhook deliveries recorded yet." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide" />
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Event
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Status
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  When
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {webhooks.map((w) => (
                <TableRow key={w.id} className="hover:bg-muted/30">
                  <TableCell>
                    {w.direction === "inbound" ? (
                      <span title="Inbound">
                        <ArrowDownToLine className="text-muted-foreground size-4" />
                      </span>
                    ) : (
                      <span title="Outbound">
                        <ArrowUpFromLine className="text-muted-foreground size-4" />
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {w.event_type || "—"}
                  </TableCell>
                  <TableCell>
                    <Badge variant={webhookStatusBadgeVariant(w.status)}>
                      {w.status}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground font-mono text-xs">
                    {formatRelative(w.created_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

// ─── Billing card (Stripe portal) ─────────────────────────────────────────

function BillingCard({
  billing,
  tenantId,
}: {
  billing: NonNullable<TenantDrilldown["billing"]>
  tenantId: string
}) {
  const [opening, setOpening] = React.useState(false)

  const openPortal = async () => {
    setOpening(true)
    try {
      const link = await api.billing.portalLink(tenantId)
      // Open in a new tab so the operator doesn't lose this page.
      window.open(link.url, "_blank", "noopener,noreferrer")
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to open Stripe portal"
      toast.error("Could not open portal", { description: message })
    } finally {
      setOpening(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle className="flex items-center gap-2 text-sm font-medium">
              <CreditCard className="size-4 text-muted-foreground" />
              Billing
            </CardTitle>
            <CardDescription>
              Stripe-backed plan + subscription snapshot for this tenant.
            </CardDescription>
          </div>
          <Button size="sm" onClick={openPortal} disabled={opening}>
            <ExternalLink />
            {opening ? "Opening…" : "Open Stripe Portal"}
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <BillingStat label="Plan" value={billing.plan} />
          <BillingStat
            label="Status"
            value={billing.subscription_status ?? "—"}
          />
          <BillingStat
            label="Period ends"
            value={
              billing.current_period_end
                ? formatRelative(billing.current_period_end)
                : "—"
            }
          />
          <BillingStat
            label="Trial ends"
            value={
              billing.trial_ends_at ? formatRelative(billing.trial_ends_at) : "—"
            }
          />
        </div>
      </CardContent>
    </Card>
  )
}

function BillingStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md border bg-card p-2">
      <span className="text-muted-foreground text-[10px] font-medium uppercase tracking-wide">
        {label}
      </span>
      <span className="font-mono text-xs">{value}</span>
    </div>
  )
}

// ─── Recent audit (free bonus — already on existing detail sheet) ─────────

function RecentAuditCard({ audit }: { audit: AuditEntry[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">
          Recent audit ({audit.length})
        </CardTitle>
        <CardDescription>
          The last ten audit entries scoped to this tenant.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {audit.length === 0 ? (
          <EmptyInline message="No audit entries for this tenant yet." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Action
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Resource
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  When
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {audit.map((entry) => (
                <TableRow key={entry.id} className="hover:bg-muted/30">
                  <TableCell>
                    <Badge variant="outline" className="font-mono text-[10px]">
                      {entry.action}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground font-mono text-xs">
                    {entry.resource_type
                      ? `${entry.resource_type}${
                          entry.resource_id ? `:${entry.resource_id}` : ""
                        }`
                      : "—"}
                  </TableCell>
                  <TableCell className="text-muted-foreground font-mono text-xs">
                    {formatRelative(entry.occurred_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

// ─── Empty-state helper ───────────────────────────────────────────────────

function EmptyInline({ message }: { message: string }) {
  return (
    <p className="text-muted-foreground py-3 text-xs">{message}</p>
  )
}