// Agents tab — agents registered with the bundled agent runtime.

import { ArrowRight, Boxes, ExternalLink, Tag, TerminalSquare } from "lucide-react"
import Link from "next/link"

import { PageHeader } from "@/components/layout/page-header"
import { GuidedTour } from "@/components/guided-tour"
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
import { api, type Harness } from "@/lib/api"

export const dynamic = "force-dynamic"

const agentFieldBase =
  process.env.NEXT_PUBLIC_RUNTIME_UI_URL ?? "http://localhost:8081"

function agentFieldCatalogUrl(agentId?: string, reasonerId?: string): string {
  const url = new URL("/api/v1/discovery/capabilities", agentFieldBase)
  if (agentId) url.searchParams.set("agent_id", agentId)
  if (reasonerId) url.searchParams.set("reasoner", reasonerId)
  return url.toString()
}

export default async function Page() {
  let list: Awaited<ReturnType<typeof api.agents>> | null = null
  let harnesses: Harness[] = []
  let error: string | null = null
  let harnessError: string | null = null
  try {
    list = await api.agents()
  } catch (e) {
    error = e instanceof Error ? e.message : String(e)
  }
  try {
    const harnessList = await api.harnesses.list()
    harnesses = harnessList.harnesses
  } catch (e) {
    harnessError = e instanceof Error ? e.message : String(e)
  }

  const agents = list?.agents ?? []
  const readyHarnesses = harnesses.filter((h) => h.status === "ready").length

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Agents"
        description="Live agent capabilities registered with the runtime."
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              nativeButton={false}
              render={
                <Link href={agentFieldCatalogUrl()} target="_blank">
                  Runtime catalog
                  <ExternalLink data-icon="inline-end" />
                </Link>
              }
            />
            <GuidedTour
              id="admin-agents-v1"
              autoStart
              steps={[
                {
                  element: "[data-tour='agents-counts']",
                  popover: {
                    title: "Live capability discovery",
                    description:
                      "These counts come from the running stack, so a fork can tell whether its support workflow is actually registered.",
                    side: "bottom",
                    align: "start",
                  },
                },
                {
                  element: "[data-tour='agent-harnesses']",
                  popover: {
                    title: "Tool readiness is part of the backend",
                    description:
                      "The admin shows whether coding tools and harnesses are available without making the customer app own that infrastructure.",
                    side: "bottom",
                    align: "start",
                  },
                },
                {
                  element: "[data-tour='agent-cards']",
                  popover: {
                    title: "SupportDesk should appear here",
                    description:
                      "The default stack registers a supportdesk workflow with reply-planning checks. Badges link out to the runtime catalog for the deeper view.",
                    side: "top",
                    align: "start",
                  },
                },
              ]}
            />
          </>
        }
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3" data-tour="agents-counts">
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <Boxes className="size-3" />
              Registered
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {agents.length}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <Tag className="size-3" />
              Reasoners
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {agents.reduce((acc, a) => acc + (a.reasoners?.length ?? 0), 0)}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <TerminalSquare className="size-3" />
              Harnesses ready
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {readyHarnesses}
              <span className="text-muted-foreground text-base font-normal">
                /{harnesses.length || 4}
              </span>
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <Card data-tour="agent-harnesses">
        <CardHeader>
          <div className="flex items-center justify-between gap-3">
            <div>
              <CardTitle className="text-base">Agent container harnesses</CardTitle>
              <CardDescription>
                Claude Code, Codex, Gemini, and OpenCode are detected from the
                agent container. The runtime only reports what registered
                agents declare.
              </CardDescription>
            </div>
            <Button
              variant="outline"
              size="sm"
              nativeButton={false}
              render={
                <Link href="/operate/runs">
                  View runs
                  <ArrowRight data-icon="inline-end" />
                </Link>
              }
            />
          </div>
        </CardHeader>
        <CardContent>
          {harnessError ? (
            <p className="text-muted-foreground text-sm">
              Harness data unavailable: {harnessError}
            </p>
          ) : harnesses.length === 0 ? (
            <p className="text-muted-foreground text-sm">
              No harness probe data reported yet.
            </p>
          ) : (
            <div className="flex flex-wrap gap-2">
              {harnesses.map((harness) => (
                <Badge
                  key={harness.provider}
                  variant={harnessStatusVariant(harness.status)}
                  className="gap-1.5"
                >
                  <span className="font-mono">{harness.provider}</span>
                  <span className="text-muted-foreground">
                    {harness.status.replace("_", " ")}
                  </span>
                </Badge>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {agents.length === 0 ? (
        <Empty data-tour="agent-cards">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Boxes />
            </EmptyMedia>
            <EmptyTitle>No agents registered</EmptyTitle>
            <EmptyDescription>
              {error
                ? `The runtime returned: ${error}.`
                : "Agents register themselves at startup. Drop one in apps/backend/agents/ and `docker compose up` will register it."}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div
          className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3"
          data-tour="agent-cards"
        >
          {agents.map((a) => (
            <Card key={a.node_id}>
              <CardHeader>
                <div className="flex items-center justify-between gap-2">
                  <CardTitle className="font-mono text-base">
                    <Link
                      href={agentFieldCatalogUrl(a.node_id)}
                      target="_blank"
                      className="inline-flex items-center gap-1 underline-offset-4 hover:underline"
                    >
                      {a.node_id}
                      <ExternalLink className="size-3" />
                    </Link>
                  </CardTitle>
                  {a.version && (
                    <Badge variant="outline">v{a.version}</Badge>
                  )}
                </div>
                <CardDescription>
                  {a.reasoners?.length ?? 0} reasoners ·{" "}
                  {a.tags?.length ?? 0} tags
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="flex flex-col gap-3">
                  {a.reasoners && a.reasoners.length > 0 && (
                    <div className="flex flex-col gap-1.5">
                      <span className="text-muted-foreground text-xs">
                        Reasoners
                      </span>
                      <div className="flex flex-wrap gap-1.5">
                        {a.reasoners.map((r) => (
                          <Badge
                            key={r}
                            variant="secondary"
                            render={
                              <Link
                                href={agentFieldCatalogUrl(a.node_id, r)}
                                target="_blank"
                                title={`Open ${a.node_id}.${r} in the runtime catalog`}
                              >
                                {r}
                                <ExternalLink className="ml-1 size-3" />
                              </Link>
                            }
                          />
                        ))}
                      </div>
                    </div>
                  )}
                  {a.tags && a.tags.length > 0 && (
                    <div className="flex flex-col gap-1.5">
                      <span className="text-muted-foreground text-xs">
                        Tags
                      </span>
                      <div className="flex flex-wrap gap-1.5">
                        {a.tags.map((t) => (
                          <Badge key={t} variant="outline">
                            {t}
                          </Badge>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}

function harnessStatusVariant(
  status: Harness["status"],
): "default" | "secondary" | "destructive" | "outline" {
  switch (status) {
    case "ready":
      return "default"
    case "needs_auth":
      return "secondary"
    case "errored":
      return "destructive"
    case "missing":
      return "outline"
  }
}
