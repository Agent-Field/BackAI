// Integrations tab — the canonical home for the native tool adapter view
// (#16). Each card represents one of the six built-in tool types
// (browser / search / fs / exec / http / sql): the active adapter
// (chosen via env), per-tenant enable toggle, and the verbs the agent
// can call via `app.mcp.call("native:<tool>", "<verb>", {...})`.
//
// Below the adapter grid we keep a directory of related surfaces — MCP
// servers, skills, harnesses — that live on their own dedicated tabs.

import {
  Bot,
  Boxes,
  Layers,
  Wrench,
} from "lucide-react"
import Link from "next/link"

import { PageHeader } from "@/components/layout/page-header"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { api } from "@/lib/api"
import { ToolAdapterGrid } from "./_components/tool-adapter-grid"

export const dynamic = "force-dynamic"

export default async function Page() {
  let mcpCount = 0
  let skillsCount = 0
  let harnessReady = 0
  let harnessTotal = 0
  let nativeTools: Awaited<ReturnType<typeof api.tools.listNative>>["tools"] = []

  try {
    const mcp = await api.mcp.servers()
    mcpCount = mcp.servers.length
  } catch {
    /* ok */
  }
  try {
    const skills = await api.skills.list()
    skillsCount = skills.skills.length
  } catch {
    /* ok */
  }
  try {
    const harnesses = await api.harnesses.list()
    harnessTotal = harnesses.harnesses.length
    harnessReady = harnesses.harnesses.filter(
      (h) => h.status === "ready",
    ).length
  } catch {
    /* ok */
  }
  try {
    const native = await api.tools.listNative()
    nativeTools = native.tools
  } catch {
    /* ok */
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Tool Adapters"
        description="Built-in agent tools: browser, search, filesystem, code exec, HTTP fetch, and read-only SQL. The active backend for each is picked via environment variables; enable per tenant here."
      />

      <ToolAdapterGrid initial={nativeTools} />

      <div className="border-t pt-6">
        <h2 className="text-base font-semibold">Related</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Other tool sources that agents can call. Each has its own
          dedicated tab.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between gap-2">
              <CardTitle className="flex items-center gap-2">
                <Wrench className="size-4" />
                MCP servers
              </CardTitle>
              <Badge variant="secondary">{mcpCount}</Badge>
            </div>
            <CardDescription>
              External Model Context Protocol servers exposing tools to
              agents over stdio or SSE.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button variant="outline" size="sm" render={<Link href="/build/mcp">Manage servers</Link>} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <div className="flex items-center justify-between gap-2">
              <CardTitle className="flex items-center gap-2">
                <Layers className="size-4" />
                Skills
              </CardTitle>
              <Badge variant="secondary">{skillsCount}</Badge>
            </div>
            <CardDescription>
              AF skillkit bundles installed into the runtime. Attach to
              agents or harnesses by id.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button variant="outline" size="sm" render={<Link href="/build/skills">Manage skills</Link>} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <div className="flex items-center justify-between gap-2">
              <CardTitle className="flex items-center gap-2">
                <Bot className="size-4" />
                Harnesses
              </CardTitle>
              <Badge variant="secondary">
                {harnessReady}/{harnessTotal} ready
              </Badge>
            </div>
            <CardDescription>
              CLI agent harnesses (Claude Code / Codex / Gemini /
              OpenCode). Probed for installation + auth status.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button variant="outline" size="sm" render={<Link href="/build/harnesses">Manage harnesses</Link>} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <div className="flex items-center justify-between gap-2">
              <CardTitle className="flex items-center gap-2">
                <Boxes className="size-4" />
                Agents
              </CardTitle>
              <Badge variant="outline">runtime</Badge>
            </div>
            <CardDescription>
              AF agents registered with the control plane at startup.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button variant="outline" size="sm" render={<Link href="/build/agents">View agents</Link>} />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
