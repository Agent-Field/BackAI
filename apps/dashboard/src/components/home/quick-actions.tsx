// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import { ArrowUpRight, Check, Code2, Copy, KeyRound, PlayCircle, Users } from "lucide-react"
import Link from "next/link"

// Quick actions — dense link list, not consumer-app card grid. Each row
// is icon + label + arrow on hover. Subordinate visual weight: KPIs
// dominate the page, this is a footer-rail equivalent.
//
// Every row here must DO something real. The original version linked
// "Issue API key" / "Test an agent" / "API explorer" at ungroomed routes
// (404s) and rendered ⌘-shortcut chips with no keyboard handler behind
// them — worse than nothing, because it reads as broken. Issue-key now
// mints inline via the same admin endpoint the welcome block uses;
// the other rows point at surfaces that exist today.

const RUNTIME_URL =
  process.env.NEXT_PUBLIC_RUNTIME_URL ?? "http://localhost:8080"

interface QuickLink {
  id: string
  label: string
  href: string
  icon: typeof Code2
  external?: boolean
}

const LINKS: QuickLink[] = [
  { id: "add-tenant", label: "Add tenant", href: "/people/tenants", icon: Users },
  { id: "view-runs", label: "View agent runs", href: "/activity/runs", icon: PlayCircle },
  {
    id: "openapi",
    label: "OpenAPI spec",
    href: `${RUNTIME_URL}/openapi.json`,
    icon: Code2,
    external: true,
  },
]

export function QuickActions() {
  return (
    <section
      aria-labelledby="quick-actions-heading"
      className="shrink-0 rounded-md border bg-card"
    >
      <header className="flex items-center justify-between border-b px-row-x py-row-y">
        <h2
          id="quick-actions-heading"
          className="text-body font-medium text-foreground"
        >
          Quick actions
        </h2>
      </header>
      <ul role="list" className="divide-y">
        <IssueKeyRow />
        {LINKS.map((link) => (
          <QuickLinkRow key={link.id} link={link} />
        ))}
      </ul>
    </section>
  )
}

// Mints a tenant API key via the operator admin API (same contract as
// the welcome block) and reveals it inline, once.
function IssueKeyRow() {
  const [state, setState] = useState<
    | { phase: "idle" }
    | { phase: "loading" }
    | { phase: "done"; value: string }
    | { phase: "error"; message: string }
  >({ phase: "idle" })
  const [copied, setCopied] = useState(false)

  const issue = async () => {
    if (state.phase === "loading") return
    setState({ phase: "loading" })
    try {
      const res = await fetch("/api/v1/admin/keys", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: "quick-action",
          tenant_id: "00000000-0000-0000-0000-000000000000",
        }),
      })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(`HTTP ${res.status}: ${text.slice(0, 120) || res.statusText}`)
      }
      const body = (await res.json()) as { value?: string }
      if (!body.value) throw new Error("response missing `value`")
      setState({ phase: "done", value: body.value })
    } catch (e) {
      setState({
        phase: "error",
        message: e instanceof Error ? e.message : String(e),
      })
    }
  }

  const copy = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // clipboard unavailable — the key is still visible to select
    }
  }

  return (
    <li>
      <button
        type="button"
        onClick={issue}
        className="group flex w-full items-center gap-stack px-row-x py-row-y text-left transition-colors hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none"
      >
        <KeyRound
          className="size-icon-inline shrink-0 text-muted-foreground"
          aria-hidden
        />
        <span className="text-body text-foreground">
          {state.phase === "loading" ? "Issuing key…" : "Issue API key"}
        </span>
        <ArrowUpRight
          aria-hidden
          className="ml-auto size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
        />
      </button>
      {state.phase === "done" ? (
        <div className="flex items-center gap-inline px-row-x pb-row-y">
          <code className="min-w-0 flex-1 truncate rounded-md border bg-muted/40 px-1.5 py-0.5 font-mono text-meta text-muted-foreground">
            {state.value}
          </code>
          <button
            type="button"
            onClick={() => copy(state.value)}
            aria-label="Copy API key"
            className="text-muted-foreground hover:text-foreground"
          >
            {copied ? (
              <Check className="size-3.5" aria-hidden />
            ) : (
              <Copy className="size-3.5" aria-hidden />
            )}
          </button>
        </div>
      ) : null}
      {state.phase === "error" ? (
        <p className="text-destructive px-row-x pb-row-y text-meta">
          {state.message}
        </p>
      ) : null}
    </li>
  )
}

function QuickLinkRow({ link }: { link: QuickLink }) {
  const Icon = link.icon
  const className =
    "group flex items-center gap-stack px-row-x py-row-y transition-colors hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none"
  const body = (
    <>
      <Icon
        className="size-icon-inline shrink-0 text-muted-foreground"
        aria-hidden
      />
      <span className="text-body text-foreground">{link.label}</span>
      <ArrowUpRight
        aria-hidden
        className="ml-auto size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
      />
    </>
  )
  return (
    <li>
      {link.external ? (
        <a href={link.href} target="_blank" rel="noreferrer" className={className}>
          {body}
        </a>
      ) : (
        <Link href={link.href} className={className}>
          {body}
        </Link>
      )}
    </li>
  )
}
