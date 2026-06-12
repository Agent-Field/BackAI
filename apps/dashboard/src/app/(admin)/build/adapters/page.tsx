// SPDX-License-Identifier: Apache-2.0

// Read-only adapter inventory. Adapter swaps stay in env/config; this page
// only makes the active choice and available options visible to operators.

import { CheckCircle2, ExternalLink, Info, SlidersHorizontal } from "lucide-react"
import Link from "next/link"

import { PageHeader } from "@/components/layout/page-header"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export const dynamic = "force-dynamic"

type AdapterStatus = "active" | "available" | "planned"

type AdapterChoice = {
  id: string
  label: string
  status: AdapterStatus
}

type AdapterPrimitive = {
  id: string
  primitive: string
  envVar: string
  active: string
  description: string
  docsHref: string
  choices: AdapterChoice[]
}

const DOCS_BASE = "https://github.com/Agent-Field/af-stack/blob/main/docs/adapters"

function clean(value: string | undefined, fallback: string): string {
  const trimmed = value?.trim().toLowerCase()
  return trimmed || fallback
}

function billingActive(): string {
  const explicit = clean(process.env.AF_STACK_BILLING_ADAPTER, "")
  if (explicit) return explicit
  return process.env.STRIPE_SECRET_KEY ? "stripe" : "none"
}

function withStatuses(active: string, shipped: string[], planned: string[]): AdapterChoice[] {
  return [...shipped, ...planned].map((id) => ({
    id,
    label: id,
    status: id === active ? "active" : shipped.includes(id) ? "available" : "planned",
  }))
}

function adapterPrimitives(): AdapterPrimitive[] {
  const storage = clean(process.env.AF_STACK_S3_ADAPTER, "minio")
  const sandbox = clean(process.env.AF_STACK_SANDBOX_ADAPTER, "docker")
  const notifications = clean(process.env.AF_STACK_NOTIFICATIONS_ADAPTER, "log")
  const billing = billingActive()

  return [
    {
      id: "storage",
      primitive: "Storage",
      envVar: "AF_STACK_S3_ADAPTER",
      active: storage,
      description: "Object storage for uploads, artifacts, and signed URLs.",
      docsHref: `${DOCS_BASE}/storage.md`,
      choices: withStatuses(storage, ["minio", "s3"], ["r2", "gcs", "azure-blob"]),
    },
    {
      id: "sandbox",
      primitive: "Sandbox",
      envVar: "AF_STACK_SANDBOX_ADAPTER",
      active: sandbox,
      description: "Ephemeral compute for code execution and agent actions.",
      docsHref: `${DOCS_BASE}/sandbox.md`,
      choices: withStatuses(sandbox, ["docker", "gvisor", "firecracker", "e2b"], ["modal"]),
    },
    {
      id: "notifications",
      primitive: "Notifications",
      envVar: "AF_STACK_NOTIFICATIONS_ADAPTER",
      active: notifications,
      description: "Outbound email, SMS, and push delivery behind one interface.",
      docsHref: `${DOCS_BASE}/notifications.md`,
      choices: withStatuses(
        notifications,
        ["log", "resend"],
        ["postmark", "sendgrid", "ses", "mailgun", "twilio", "fcm", "onesignal"],
      ),
    },
    {
      id: "billing",
      primitive: "Billing",
      envVar: "AF_STACK_BILLING_ADAPTER",
      active: billing,
      description: "Customer billing and metered usage provider.",
      docsHref: `${DOCS_BASE}/billing.md`,
      choices: withStatuses(billing, ["none", "stripe"], ["lago"]),
    },
  ]
}

function statusVariant(status: AdapterStatus): "default" | "secondary" | "outline" {
  if (status === "active") return "default"
  if (status === "available") return "secondary"
  return "outline"
}

function statusLabel(status: AdapterStatus): string {
  if (status === "active") return "active"
  if (status === "available") return "available"
  return "planned"
}

export default async function AdaptersPage() {
  const rows = adapterPrimitives()
  const activeCount = rows.length
  const availableCount = rows.reduce(
    (sum, row) => sum + row.choices.filter((choice) => choice.status === "available").length,
    0,
  )
  const plannedCount = rows.reduce(
    (sum, row) => sum + row.choices.filter((choice) => choice.status === "planned").length,
    0,
  )

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Adapters"
        description="Read-only inventory of active backend adapters. Change adapters in env or config, then restart the runtime."
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <CheckCircle2 className="size-3" />
              Active primitives
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {activeCount}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <SlidersHorizontal className="size-3" />
              Available swaps
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {availableCount}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <Info className="size-3" />
              Planned choices
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {plannedCount}
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Adapter choices</CardTitle>
          <CardDescription>
            These are config-level swaps, not dashboard actions. The operator changes env vars in
            the fork or deploy target and restarts.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Primitive</TableHead>
                <TableHead>Active</TableHead>
                <TableHead>Choices</TableHead>
                <TableHead>Env</TableHead>
                <TableHead className="text-right">Docs</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.id}>
                  <TableCell>
                    <div className="flex flex-col gap-1">
                      <span className="font-medium">{row.primitive}</span>
                      <span className="text-muted-foreground max-w-[20rem] text-xs whitespace-normal">
                        {row.description}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant="default">{row.active}</Badge>
                  </TableCell>
                  <TableCell>
                    <div className="flex max-w-[28rem] flex-wrap gap-1.5">
                      {row.choices.map((choice) => (
                        <Badge
                          key={choice.id}
                          variant={statusVariant(choice.status)}
                          title={`${choice.label}: ${statusLabel(choice.status)}`}
                        >
                          {choice.label}
                        </Badge>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell>
                    <code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs">
                      {row.envVar}
                    </code>
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="outline"
                      size="sm"
                      render={
                        <Link href={row.docsHref} target="_blank">
                          Configure
                          <ExternalLink data-icon="inline-end" />
                        </Link>
                      }
                    />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}
