// SandboxesConfigView — read-only summary of the sandbox adapter
// configuration as exposed by `api.sandbox.pool()`.
//
// This is the "Build" surface for the sandbox module: it tells a developer
// which adapter is wired in, what each adapter is good for, how to switch
// adapters in `config.yaml`, what each network mode permits, and what the
// resource defaults are. Activity / live runs live on the Operate tab.

import {
  AlertTriangle,
  CheckCircle2,
  Cpu,
  Globe,
  HardDrive,
  Lock,
  Network,
  Server,
  ShieldCheck,
  Timer,
  XCircle,
} from "lucide-react"

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
import type { SandboxPoolStats } from "@/lib/api"

type SandboxesConfigViewProps = {
  pool: SandboxPoolStats | null
  error: string | null
}

const ADAPTER_DESCRIPTIONS: Record<
  string,
  { label: string; summary: string }
> = {
  docker: {
    label: "Docker",
    summary:
      "Container-level isolation. Best for local dev and trusted workloads.",
  },
  gvisor: {
    label: "gVisor",
    summary:
      "User-space kernel between the workload and the host. Stronger isolation than plain containers.",
  },
  firecracker: {
    label: "Firecracker",
    summary:
      "MicroVM isolation. Strongest isolation, slightly higher cold start.",
  },
  e2b: {
    label: "E2B",
    summary:
      "Managed sandbox provider. Offloads infra; egress and quotas apply.",
  },
}

const ADAPTER_OPTIONS = ["docker", "gvisor", "firecracker", "e2b"] as const

const NETWORK_MODES: {
  mode: "open" | "restricted" | "isolated"
  icon: React.ComponentType<{ className?: string }>
  description: string
  egress: string
}[] = [
  {
    mode: "open",
    icon: Globe,
    description:
      "Workload can reach any host. Use only for trusted code or quick experiments.",
    egress: "Any host",
  },
  {
    mode: "restricted",
    icon: ShieldCheck,
    description:
      "Egress is allowed only to the hosts listed in allow_egress on the run request.",
    egress: "allow_egress list",
  },
  {
    mode: "isolated",
    icon: Lock,
    description:
      "No outbound network. The workload can only read what's mounted in.",
    egress: "None",
  },
]

const RESOURCE_DEFAULTS: { label: string; value: string; note: string }[] = [
  {
    label: "Timeout",
    value: "300s",
    note: "Override per run with timeout_s (max set by adapter capability).",
  },
  { label: "CPU", value: "2 cores", note: "Override per run with cpu." },
  {
    label: "Memory",
    value: "4 GB",
    note: "Override per run with memory_gb.",
  },
  {
    label: "Network",
    value: "restricted",
    note: "Override per run with network.",
  },
]

function formatTimeout(seconds: number): string {
  if (seconds <= 0) return "—"
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`
  if (seconds < 86_400) return `${Math.round(seconds / 3600)}h`
  return `${Math.round(seconds / 86_400)}d`
}

export function SandboxesConfigView({ pool, error }: SandboxesConfigViewProps) {
  if (!pool) {
    return (
      <Empty className="border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <AlertTriangle />
          </EmptyMedia>
          <EmptyTitle>Sandbox module unreachable</EmptyTitle>
          <EmptyDescription>
            {error
              ? error
              : "Enable the sandbox module in config.yaml and restart the runtime."}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  const adapterInfo =
    ADAPTER_DESCRIPTIONS[pool.adapter] ?? {
      label: pool.adapter,
      summary: "Custom adapter wired in via the suite configuration.",
    }

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <CardHeader>
          <CardDescription className="flex items-center gap-1.5">
            <Server className="size-3" />
            Active adapter
          </CardDescription>
          <div className="flex flex-wrap items-center gap-3">
            <CardTitle className="font-mono text-3xl font-semibold">
              {pool.adapter}
            </CardTitle>
            <Badge variant="secondary" className="font-medium">
              {adapterInfo.label}
            </Badge>
          </div>
          <p className="text-muted-foreground mt-2 text-sm">
            {adapterInfo.summary}
          </p>
        </CardHeader>
        <CardContent>
          <h3 className="text-muted-foreground mb-3 text-[10px] font-medium uppercase tracking-wide">
            Capabilities
          </h3>
          <div className="rounded-xl border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Capability
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Value
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Notes
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <CapabilityRow
                  icon={Timer}
                  label="Max timeout"
                  value={formatTimeout(pool.capabilities.max_timeout_s)}
                  notes="Upper bound on a single run's wall-clock duration."
                />
                <CapabilityRow
                  icon={Timer}
                  label="Cold start"
                  value={`${pool.capabilities.cold_start_ms} ms`}
                  notes="Typical time from accept to first command byte."
                />
                <CapabilityRow
                  icon={Cpu}
                  label="GPU"
                  value={pool.capabilities.supports_gpu ? "supported" : "off"}
                  enabled={pool.capabilities.supports_gpu}
                  notes="Whether workloads can request GPU acceleration."
                />
                <CapabilityRow
                  icon={Network}
                  label="Network"
                  value={
                    pool.capabilities.supports_network ? "supported" : "off"
                  }
                  enabled={pool.capabilities.supports_network}
                  notes="Whether outbound network is reachable at all."
                />
                <CapabilityRow
                  icon={HardDrive}
                  label="Mounts"
                  value={
                    pool.capabilities.supports_mounts ? "supported" : "off"
                  }
                  enabled={pool.capabilities.supports_mounts}
                  notes="Workspace / artifact volume mounts."
                />
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Switch adapter</CardTitle>
          <CardDescription>
            Adapters are selected at the suite level. Edit{" "}
            <span className="font-mono">config.yaml</span> and restart the
            runtime — the live pool will redrain on the new backend.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <pre className="bg-muted overflow-x-auto rounded-md p-3 text-left font-mono text-xs">
{`modules:
  adapters:
    sandbox: ${ADAPTER_OPTIONS.join(" | ")}`}
          </pre>
          <pre className="bg-muted overflow-x-auto rounded-md p-3 text-left font-mono text-xs">
            docker compose restart runtime
          </pre>
          <div className="flex flex-wrap gap-2">
            {ADAPTER_OPTIONS.map((a) => (
              <Badge
                key={a}
                variant={a === pool.adapter ? "default" : "outline"}
                className="font-mono"
              >
                {a}
              </Badge>
            ))}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Network policies</CardTitle>
          <CardDescription>
            Set per-run via the <span className="font-mono">network</span> and{" "}
            <span className="font-mono">allow_egress</span> fields on the run
            request.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="rounded-xl border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Mode
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Egress allowed
                  </TableHead>
                  <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                    Description
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {NETWORK_MODES.map((mode) => {
                  const Icon = mode.icon
                  return (
                    <TableRow key={mode.mode}>
                      <TableCell>
                        <Badge variant="outline" className="gap-1 font-mono">
                          <Icon className="size-3" />
                          {mode.mode}
                        </Badge>
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {mode.egress}
                      </TableCell>
                      <TableCell className="text-muted-foreground text-xs">
                        {mode.description}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Resource defaults</CardTitle>
          <CardDescription>
            These apply when a run request omits the field. The adapter
            enforces its own ceilings on top.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {RESOURCE_DEFAULTS.map((d) => (
              <div
                key={d.label}
                className="rounded-lg border bg-card p-3"
              >
                <div className="text-muted-foreground text-[10px] font-medium uppercase tracking-wide">
                  {d.label}
                </div>
                <div className="font-mono text-lg font-semibold tabular-nums">
                  {d.value}
                </div>
                <p className="text-muted-foreground mt-1 text-xs">{d.note}</p>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function CapabilityRow({
  icon: Icon,
  label,
  value,
  notes,
  enabled,
}: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: string
  notes: string
  enabled?: boolean
}) {
  return (
    <TableRow>
      <TableCell>
        <div className="flex items-center gap-2">
          <Icon className="text-muted-foreground size-3.5" />
          <span className="text-xs font-medium">{label}</span>
        </div>
      </TableCell>
      <TableCell>
        <div className="flex items-center gap-1.5">
          {enabled === true ? (
            <CheckCircle2 className="size-3.5 text-emerald-600 dark:text-emerald-400" />
          ) : enabled === false ? (
            <XCircle className="text-muted-foreground size-3.5" />
          ) : null}
          <span className="font-mono text-xs">{value}</span>
        </div>
      </TableCell>
      <TableCell className="text-muted-foreground text-xs">{notes}</TableCell>
    </TableRow>
  )
}
