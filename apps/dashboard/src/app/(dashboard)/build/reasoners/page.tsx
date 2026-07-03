// SPDX-License-Identifier: Apache-2.0

import { X } from "lucide-react"
import Link from "next/link"

import { ReasonersTable } from "@/components/reasoners-page/reasoners-table"
import { WindowChips } from "@/components/reasoners-page/window-chips"

import { fetchReasonersSnapshot } from "@/lib/reasoners-page/data"
import {
  isReasonerWindowKind,
  type ReasonerWindowKind,
} from "@/lib/reasoners-page/types"

// Build → Reasoners. Per-reasoner cost / latency / error analytics,
// server-rendered with URL-driven window (?window=) and agent
// (?agent=) filters. The window chips replace the search param and the
// force-dynamic page re-fetches with the widened `from`.

export const dynamic = "force-dynamic"

export default async function ReasonersPage({
  searchParams,
}: {
  searchParams: Promise<{ window?: string; agent?: string }>
}) {
  const sp = await searchParams
  const window: ReasonerWindowKind =
    sp.window && isReasonerWindowKind(sp.window) ? sp.window : "24h"
  const agent = sp.agent?.trim() || undefined
  const snapshot = await fetchReasonersSnapshot(window, agent)
  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex flex-wrap items-baseline justify-between gap-stack">
        <div className="flex flex-col gap-tile-tight">
          <h1 className="text-xl font-semibold tracking-tight text-foreground">
            Reasoners
          </h1>
          <p className="text-meta text-muted-foreground">
            Cost, latency, and error rates per reasoner — attributed via the
            X-AF-Reasoner header on gateway calls.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-stack">
          {agent ? <AgentFilterChip agent={agent} window={window} /> : null}
          <WindowChips window={window} />
        </div>
      </header>
      <ReasonersTable
        rows={snapshot.rows}
        healthy={snapshot.healthy}
        agentFiltered={Boolean(agent)}
      />
    </div>
  )
}

// Active ?agent= filter rendered as an inverted chip; clicking clears
// the filter but keeps the selected window. Server-rendered Link — no
// client shell needed for a one-shot clear.
function AgentFilterChip({
  agent,
  window,
}: {
  agent: string
  window: ReasonerWindowKind
}) {
  const clearHref =
    window === "24h" ? "/build/reasoners" : `/build/reasoners?window=${window}`
  return (
    <Link
      href={clearHref}
      aria-label={`Clear agent filter ${agent}`}
      className="inline-flex h-6 shrink-0 items-center gap-inline rounded-md border border-foreground bg-foreground px-pill-x text-meta leading-none text-background transition-colors hover:bg-foreground/90"
    >
      <span className="font-mono">{agent}</span>
      <X aria-hidden className="size-3" />
    </Link>
  )
}
