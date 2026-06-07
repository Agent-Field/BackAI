// SPDX-License-Identifier: Apache-2.0

// Inline adapter + capabilities indicator.
//
// Shows which adapter the runtime is configured with, and exposes the
// capability flags as compact Badges. Each badge has a Tooltip explaining
// what the flag means in plain English so a developer who's just landed
// on the page can orient themselves without reading docs.

import {
  Cpu,
  HardDrive,
  Network,
  Settings2,
  Timer,
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
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import type { SandboxCapabilities } from "@/lib/api"

type SandboxAdapterCardProps = {
  adapter: string
  capabilities: SandboxCapabilities
}

function formatTimeout(seconds: number): string {
  if (seconds <= 0) return "—"
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`
  if (seconds < 86_400) return `${Math.round(seconds / 3600)}h`
  return `${Math.round(seconds / 86_400)}d`
}

function CapabilityBadge({
  label,
  enabled,
  icon: Icon,
  tooltip,
}: {
  label: string
  enabled: boolean
  icon: React.ComponentType<{ className?: string }>
  tooltip: string
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Badge variant={enabled ? "secondary" : "ghost"} className="gap-1">
            <Icon className="size-3" />
            {label}
            <span
              className={
                enabled
                  ? "text-emerald-600 dark:text-emerald-400"
                  : "text-muted-foreground"
              }
            >
              {enabled ? "on" : "off"}
            </span>
          </Badge>
        }
      />
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  )
}

export function SandboxAdapterCard({
  adapter,
  capabilities,
}: SandboxAdapterCardProps) {
  return (
    <Card>
      <CardHeader>
        <CardDescription className="flex items-center gap-1.5">
          <Settings2 className="size-3" />
          Adapter
        </CardDescription>
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle className="font-mono text-2xl font-semibold">
            {adapter}
          </CardTitle>
          <Badge variant="outline" className="font-mono">
            cold start ≈ {capabilities.cold_start_ms}ms
          </Badge>
        </div>
      </CardHeader>
      <CardContent>
        <div className="flex flex-wrap gap-2">
          <Tooltip>
            <TooltipTrigger
              render={
                <Badge variant="outline" className="gap-1 font-mono">
                  <Timer className="size-3" />
                  max {formatTimeout(capabilities.max_timeout_s)}
                </Badge>
              }
            />
            <TooltipContent>
              Maximum wall-clock duration a single sandbox run is allowed
              before the adapter terminates it.
            </TooltipContent>
          </Tooltip>
          <CapabilityBadge
            label="GPU"
            enabled={capabilities.supports_gpu}
            icon={Cpu}
            tooltip="Whether workloads can request GPU acceleration."
          />
          <CapabilityBadge
            label="Network"
            enabled={capabilities.supports_network}
            icon={Network}
            tooltip="Whether workloads can reach the network. Egress policy is set per-run."
          />
          <CapabilityBadge
            label="Mounts"
            enabled={capabilities.supports_mounts}
            icon={HardDrive}
            tooltip="Whether workspace and artifact volume mounts are supported."
          />
        </div>
      </CardContent>
    </Card>
  )
}