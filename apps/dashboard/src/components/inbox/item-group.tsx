// SPDX-License-Identifier: Apache-2.0

"use client"

import { ChevronRight, KeyRound, ShieldAlert } from "lucide-react"

import type { InboxItem, InboxSeverity } from "@/lib/inbox/types"

// One severity tier — header + items. Severity color is restricted to the
// header label + the per-item dot, not the surrounding card chrome. This
// keeps the page calm: red only where it means "act."

const SEVERITY_LABEL: Record<InboxSeverity, string> = {
  critical: "Critical",
  warning: "Warning",
  info: "Info",
}

const SEVERITY_DOT: Record<InboxSeverity, string> = {
  critical: "bg-destructive",
  warning: "bg-warning",
  info: "bg-muted-foreground/40",
}

const SEVERITY_TEXT: Record<InboxSeverity, string> = {
  critical: "text-destructive",
  warning: "text-warning",
  info: "text-muted-foreground",
}

interface ItemGroupProps {
  severity: InboxSeverity
  items: InboxItem[]
  onRowClick: (item: InboxItem) => void
  selectedId: string | null
}

export function ItemGroup({
  severity,
  items,
  onRowClick,
  selectedId,
}: ItemGroupProps) {
  return (
    <section
      aria-labelledby={`inbox-group-${severity}`}
      className="rounded-md border bg-card"
    >
      <header className="flex items-center justify-between border-b px-row-x py-row-y">
        <h2
          id={`inbox-group-${severity}`}
          className="flex items-center gap-inline text-body font-medium"
        >
          <span
            aria-hidden
            className={`inline-block size-icon-dot rounded-pill ${SEVERITY_DOT[severity]}`}
          />
          <span className={SEVERITY_TEXT[severity]}>
            {SEVERITY_LABEL[severity]}
          </span>
        </h2>
        <span className="text-meta text-muted-foreground tabular-nums">
          {items.length}
        </span>
      </header>
      <ul role="list" className="divide-y">
        {items.map((item) => (
          <ItemRow
            key={item.id}
            item={item}
            selected={item.id === selectedId}
            onClick={() => onRowClick(item)}
          />
        ))}
      </ul>
    </section>
  )
}

function ItemRow({
  item,
  selected,
  onClick,
}: {
  item: InboxItem
  selected: boolean
  onClick: () => void
}) {
  const Icon = item.kind === "approval" ? KeyRound : ShieldAlert
  const clickable = item.kind === "approval"
  return (
    <li>
      <button
        type="button"
        onClick={clickable ? onClick : undefined}
        disabled={!clickable}
        aria-pressed={selected}
        className={`group flex w-full items-start gap-stack px-row-x py-tile text-left transition-colors ${
          clickable
            ? "hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none"
            : "cursor-default"
        } ${selected ? "bg-accent/30" : ""}`}
      >
        <Icon
          className="mt-0.5 size-icon-inline shrink-0 text-muted-foreground"
          aria-hidden
        />
        <div className="flex min-w-0 flex-1 flex-col gap-tile-tight">
          <div className="flex items-center gap-stack">
            <span className="min-w-0 truncate text-body text-foreground">
              {item.title}
            </span>
            <span className="ml-auto shrink-0 text-meta tabular-nums text-muted-foreground">
              {formatRelative(item.occurredAt)}
            </span>
          </div>
          {item.context ? (
            <span className="line-clamp-2 text-meta text-muted-foreground">
              {item.context}
            </span>
          ) : null}
          <div className="flex items-center gap-inline text-meta text-muted-foreground">
            <span className="font-mono uppercase tracking-wide">
              {item.kind}
            </span>
            {item.kind === "alert" ? (
              <span className="font-mono uppercase tracking-wide">
                · resolves when condition clears
              </span>
            ) : null}
          </div>
        </div>
        {clickable ? (
          <ChevronRight
            aria-hidden
            className="mt-1 size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
          />
        ) : null}
      </button>
    </li>
  )
}

function formatRelative(iso: string): string {
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = Date.now() - ts
  const sec = Math.max(0, Math.round(diffMs / 1000))
  if (sec < 30) return "now"
  if (sec < 3600) return `${Math.round(sec / 60)}m`
  if (sec < 86_400) return `${Math.round(sec / 3600)}h`
  const days = Math.round(sec / 86_400)
  if (days < 7) return `${days}d`
  return new Date(ts).toISOString().slice(5, 10)
}
