// SPDX-License-Identifier: Apache-2.0

import {
  ERROR_RATE_ALERT,
  formatErrorRatePct,
  formatReasonerCost,
} from "@/lib/reasoners-page/derive"
import type { ReasonerRow } from "@/lib/reasoners-page/types"
import { formatRunAge, formatRunDuration } from "@/lib/runs/derive"

// Reasoner analytics table — sticky header, one row per agent.reasoner
// pair. Server-rendered; loading / empty / degraded states share the
// same column shell per the framework's "structure visible even at
// zero" rule.

const REASONER_ROW_COLUMNS =
  "grid-cols-[minmax(0,1fr)_64px_128px_96px_96px_96px]"

interface ReasonersTableProps {
  rows: ReasonerRow[]
  healthy: boolean
  agentFiltered: boolean
}

export function ReasonersTable({
  rows,
  healthy,
  agentFiltered,
}: ReasonersTableProps) {
  return (
    <section
      aria-label="Reasoner analytics table"
      className="flex min-h-0 flex-col rounded-md border bg-card"
    >
      <TableHeader total={rows.length} />
      {!healthy ? (
        <DegradedRow />
      ) : rows.length === 0 ? (
        <EmptyRow agentFiltered={agentFiltered} />
      ) : (
        <ul role="list" className="flex flex-col">
          {rows.map((row) => (
            <li
              key={`${row.agent}.${row.reasoner}`}
              className={`grid items-center gap-stack border-b px-row-x py-row-y text-meta last:border-b-0 ${REASONER_ROW_COLUMNS}`}
            >
              <span
                className="truncate font-mono text-body text-foreground"
                title={`${row.agent}.${row.reasoner}`}
              >
                {row.agent}.{row.reasoner}
              </span>
              <span className="text-right font-mono tabular-nums text-foreground">
                {row.calls}
              </span>
              <span
                className={`text-right font-mono tabular-nums ${
                  row.error_rate > ERROR_RATE_ALERT
                    ? "text-destructive"
                    : "text-muted-foreground"
                }`}
              >
                {row.errors} ({formatErrorRatePct(row.error_rate)})
              </span>
              <span className="text-right font-mono tabular-nums text-muted-foreground">
                {formatRunDuration(row.avg_latency_ms)}
              </span>
              <span className="text-right font-mono tabular-nums text-muted-foreground">
                {formatReasonerCost(row.cost_usd)}
              </span>
              <span className="text-right font-mono tabular-nums text-muted-foreground">
                {row.last_called_at ? formatRunAge(row.last_called_at) : "—"}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function TableHeader({ total }: { total: number }) {
  return (
    <header
      className={`sticky top-0 z-10 grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${REASONER_ROW_COLUMNS}`}
    >
      <span>Reasoner</span>
      <span className="text-right">Calls</span>
      <span className="text-right">Errors</span>
      <span className="text-right">Avg latency</span>
      <span className="text-right">Cost</span>
      <span className="text-right tabular-nums">
        {total > 0 ? `Last called · ${total}` : "Last called"}
      </span>
    </header>
  )
}

function EmptyRow({ agentFiltered }: { agentFiltered: boolean }) {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">
        {agentFiltered
          ? "No reasoner calls for this agent in this window."
          : "No reasoner calls in this window."}
      </p>
      <p className="max-w-md text-meta text-muted-foreground">
        Calls through{" "}
        <code className="font-mono">/api/v1/llm/chat/completions</code> with
        an <code className="font-mono">X-AF-Reasoner</code> header are
        attributed here.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">
        Reasoner analytics unavailable.
      </p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return analytics. Check the Health page for a
        database probe, then reload.
      </p>
    </div>
  )
}
