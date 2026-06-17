// SPDX-License-Identifier: Apache-2.0

import { ArrowUpRight, Code2, KeyRound, PlayCircle, Users } from "lucide-react"
import Link from "next/link"

// Quick actions — dense link list, not consumer-app card grid. Each row
// is icon + label + shortcut hint + arrow on hover. Subordinate visual
// weight: KPIs dominate the page, this is a footer-rail equivalent.

interface QuickAction {
  id: string
  label: string
  href: string
  icon: typeof Code2
  shortcut?: string
}

const ACTIONS: QuickAction[] = [
  { id: "issue-key", label: "Issue API key", href: "/people/keys", icon: KeyRound, shortcut: "⌘I" },
  { id: "add-tenant", label: "Add tenant", href: "/people/tenants", icon: Users, shortcut: "⌘T" },
  { id: "test-agent", label: "Test an agent", href: "/build/agents", icon: PlayCircle, shortcut: "⌘P" },
  { id: "api-explorer", label: "Open API explorer", href: "/platform/api", icon: Code2 },
]

export function QuickActions() {
  return (
    <section
      aria-labelledby="quick-actions-heading"
      className="rounded-md border bg-card"
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
        {ACTIONS.map((action) => (
          <QuickActionRow key={action.id} action={action} />
        ))}
      </ul>
    </section>
  )
}

function QuickActionRow({ action }: { action: QuickAction }) {
  const Icon = action.icon
  return (
    <li>
      <Link
        href={action.href}
        className="group flex items-center gap-stack px-row-x py-row-y transition-colors hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none"
      >
        <Icon
          className="size-icon-inline shrink-0 text-muted-foreground"
          aria-hidden
        />
        <span className="text-body text-foreground">{action.label}</span>
        <span className="ml-auto inline-flex items-center gap-inline">
          {action.shortcut ? (
            <span className="rounded-md border px-1.5 py-0.5 font-mono text-meta text-muted-foreground">
              {action.shortcut}
            </span>
          ) : null}
          <ArrowUpRight
            aria-hidden
            className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
          />
        </span>
      </Link>
    </li>
  )
}
