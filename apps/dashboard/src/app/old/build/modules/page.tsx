// Modules tab — read-only view of the runtime's config.yaml state.
//
// Pulls from /api/v1/modules. Each module is rendered as a row with its
// enable flag + a description; workload modules get their own section.

import { Layers, Power, PowerOff } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import { Badge } from "@/components/ui/badge"
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
import { api } from "@/lib/api"

export const dynamic = "force-dynamic"

const MODULE_DESCRIPTIONS: Record<string, string> = {
  identity: "Operator + customer authentication via better-auth",
  "public-gateway": "OpenAI-compatible REST gateway with rate limiting",
  "llm-gateway": "Multi-provider LLM routing with cost ledger + cache",
  jobs: "River-backed background job queue",
  "secrets-vault": "AES-256-GCM encrypted secret storage",
  storage: "Object storage adapter (MinIO or S3)",
  notifications: "Outbox-style notification delivery",
  "webhooks-in": "Inbound webhooks with HMAC + dedup",
  "webhooks-out": "Outbound webhook delivery with retry",
  billing: "Stripe-integrated billing with usage meters",
  observability: "Prometheus + OTel + structured logs",
  "mcp-client": "Model Context Protocol server host",
  "multi-tenancy": "PG-RLS-backed tenant isolation",
  sandbox: "Isolated code execution (docker / gvisor / firecracker / e2b)",
  memory: "Vector memory store backed by pgvector",
  crons: "Scheduled job dispatcher",
  skills: "Installed AF skillkit bundles",
  harnesses:
    "CLI agent harnesses (Claude Code / Codex / Gemini / OpenCode)",
}

export default async function Page() {
  let state: Awaited<ReturnType<typeof api.modulesState>> | null = null
  let error: string | null = null
  try {
    state = await api.modulesState()
  } catch (e) {
    error = e instanceof Error ? e.message : String(e)
  }

  if (!state) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Modules"
          description="Live view of the runtime's enabled modules and workload modules."
        />
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Layers />
            </EmptyMedia>
            <EmptyTitle>Modules state unreachable</EmptyTitle>
            <EmptyDescription>
              {error ?? "The runtime did not return a modules list."}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  const enabledCount = state.modules.filter((m) => m.enabled).length
  const disabledCount = state.modules.length - enabledCount

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Modules"
        description="Live view of the runtime's enabled modules and workload modules. Sourced from /api/v1/modules."
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <Power className="size-3" />
              Enabled
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {enabledCount}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <PowerOff className="size-3" />
              Disabled
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {disabledCount}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <Layers className="size-3" />
              Multi-tenancy
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {state.multi_tenancy_enabled ? "On" : "Off"}
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Built-in modules</CardTitle>
          <CardDescription>
            Flip via <code className="font-mono">config.yaml</code> or
            <code className="font-mono"> AF_STACK_MODULE_*=true|false</code>{" "}
            env overrides, then restart the runtime.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Module</TableHead>
                <TableHead>Description</TableHead>
                <TableHead className="w-24">Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {state.modules.map((m) => (
                <TableRow key={m.id}>
                  <TableCell>
                    <div className="flex flex-col gap-0.5">
                      <span className="font-medium">{m.name ?? m.id}</span>
                      <span className="text-muted-foreground font-mono text-xs">
                        {m.id}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell className="text-muted-foreground text-sm">
                    {MODULE_DESCRIPTIONS[m.id] ?? "—"}
                  </TableCell>
                  <TableCell>
                    <Badge variant={m.enabled ? "default" : "outline"}>
                      {m.enabled ? "Enabled" : "Disabled"}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {state.workload_modules.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Workload modules</CardTitle>
            <CardDescription>
              Domain-specific feature packs loaded from{" "}
              <code className="font-mono">workload-modules/</code>.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-2">
              {state.workload_modules.map((wm) => (
                <Badge key={wm} variant="secondary">
                  {wm}
                </Badge>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
