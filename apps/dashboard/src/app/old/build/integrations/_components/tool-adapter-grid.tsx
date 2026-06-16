"use client"

// ToolAdapterGrid — one card per native tool (browser / search / fs /
// exec / http / sql). Each card surfaces:
//   - active adapter id (from env, read-only)
//   - "configured" badge — whether the adapter's required env vars are set
//   - per-tenant enable switch
//   - verb list (collapsed) so operators see what agents can call
//   - "Test" button: invokes verb #1 with empty args (best-effort) so
//     operators can confirm the wiring without leaving the dashboard.
//
// Per-tenant config (config blob) is left for later — for now we just
// toggle enabled. The schema supports it; the UI doesn't expose it yet.

import * as React from "react"
import {
  Beaker,
  Bot,
  ChevronDown,
  ChevronRight,
  Code,
  Database,
  Globe,
  HardDrive,
  Search,
} from "lucide-react"
import { toast } from "sonner"

import {
  api,
  ApiError,
  type NativeToolName,
  type NativeToolStatus,
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
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Switch } from "@/components/ui/switch"

const TOOL_ICONS: Record<NativeToolName, React.ComponentType<{ className?: string }>> = {
  browser: Globe,
  search: Search,
  fs: HardDrive,
  exec: Code,
  http: Globe,
  sql: Database,
}

// TOOL_DOCS points to operator documentation per tool type. The docs
// site owns the full reference; we link out rather than duplicate the
// env-var matrix in the UI.
const TOOL_DOCS: Record<NativeToolName, string> = {
  browser: "/docs/tools/browser",
  search: "/docs/tools/search",
  fs: "/docs/tools/fs",
  exec: "/docs/tools/exec",
  http: "/docs/tools/http",
  sql: "/docs/tools/sql",
}

interface Props {
  initial: NativeToolStatus[]
}

export function ToolAdapterGrid({ initial }: Props) {
  const [tools, setTools] = React.useState<NativeToolStatus[]>(initial)
  const [savingTool, setSavingTool] = React.useState<NativeToolName | null>(
    null,
  )
  const [testingTool, setTestingTool] = React.useState<NativeToolName | null>(
    null,
  )

  if (tools.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Bot className="size-4" />
            Native tools not configured
          </CardTitle>
          <CardDescription>
            The runtime did not register any native tools. Check the
            startup logs for adapter selection errors (e.g.
            <code className="mx-1 rounded bg-muted px-1 py-0.5">
              AF_STACK_TOOL_BROWSER
            </code>
            pointing at an adapter without its required env vars).
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  async function setEnabled(tool: NativeToolName, enabled: boolean) {
    setSavingTool(tool)
    try {
      const updated = await api.tools.enableNative(tool, { enabled })
      setTools((prev) =>
        prev.map((t) => (t.tool === tool ? updated : t)),
      )
      toast.success(
        `${tool} ${enabled ? "enabled" : "disabled"} for this tenant`,
      )
    } catch (err) {
      const msg =
        err instanceof ApiError ? err.message : "failed to update tool"
      toast.error(msg)
    } finally {
      setSavingTool(null)
    }
  }

  async function testTool(tool: NativeToolStatus) {
    if (tool.verbs.length === 0) {
      toast.error("no verbs to test")
      return
    }
    const verb = tool.verbs[0].name
    setTestingTool(tool.tool)
    try {
      const result = await api.tools.invokeNative(tool.tool, {
        verb,
        args: {},
      })
      toast.success(`tested ${tool.tool}.${verb}`, {
        description: JSON.stringify(result.result).slice(0, 200),
      })
    } catch (err) {
      const msg =
        err instanceof ApiError ? err.message : "test invocation failed"
      toast.error(`test ${tool.tool}.${verb}: ${msg}`)
    } finally {
      setTestingTool(null)
    }
  }

  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
      {tools.map((tool) => {
        const Icon = TOOL_ICONS[tool.tool] ?? Bot
        const docHref = TOOL_DOCS[tool.tool] ?? "/docs"
        return (
          <Card key={tool.tool}>
            <CardHeader>
              <div className="flex items-center justify-between gap-2">
                <CardTitle className="flex items-center gap-2 capitalize">
                  <Icon className="size-4" />
                  {tool.tool}
                </CardTitle>
                <div className="flex items-center gap-2">
                  <Badge
                    variant={tool.configured ? "secondary" : "outline"}
                  >
                    {tool.configured ? "configured" : "not configured"}
                  </Badge>
                </div>
              </div>
              <CardDescription>{tool.description}</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <div className="flex items-center justify-between rounded-md border bg-muted/30 px-3 py-2">
                <div className="flex flex-col">
                  <span className="text-xs font-medium text-muted-foreground">
                    Active adapter
                  </span>
                  <code className="text-sm">{tool.adapter_id || "—"}</code>
                </div>
                <Switch
                  id={`enable-${tool.tool}`}
                  checked={tool.enabled}
                  disabled={savingTool === tool.tool}
                  onCheckedChange={(next) =>
                    void setEnabled(tool.tool, next)
                  }
                />
              </div>

              <Collapsible>
                <CollapsibleTrigger
                  className="flex items-center gap-1 text-xs font-medium text-muted-foreground"
                  data-state-icon="true"
                >
                  <ChevronRight className="size-3 data-[state=open]:hidden" />
                  <ChevronDown className="size-3 hidden data-[state=open]:inline" />
                  {tool.verbs.length} verb{tool.verbs.length === 1 ? "" : "s"}
                </CollapsibleTrigger>
                <CollapsibleContent className="mt-2 flex flex-col gap-1.5">
                  {tool.verbs.map((v) => (
                    <div
                      key={v.name}
                      className="rounded-sm border bg-background px-2 py-1.5 text-xs"
                    >
                      <code className="font-mono">{v.name}</code>
                      <p className="mt-0.5 text-muted-foreground">
                        {v.description}
                      </p>
                    </div>
                  ))}
                </CollapsibleContent>
              </Collapsible>

              <div className="flex items-center justify-between gap-2 pt-1">
                <a
                  href={docHref}
                  className="text-xs text-muted-foreground underline-offset-2 hover:underline"
                  target="_blank"
                  rel="noreferrer"
                >
                  Configure
                </a>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={
                    !tool.configured ||
                    !tool.enabled ||
                    testingTool === tool.tool
                  }
                  onClick={() => void testTool(tool)}
                >
                  <Beaker className="size-3" data-icon="inline-start" />
                  Test
                </Button>
              </div>
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}
