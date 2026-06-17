// SPDX-License-Identifier: Apache-2.0

import { ArrowRight, Code2, KeyRound, PlayCircle, Users } from "lucide-react"
import Link from "next/link"

import {
  Card,
  CardContent,
  CardDescription,
  CardTitle,
} from "@/components/ui/card"

// Four quick-action cards per home.md §20 option 2 ("four on Home"). Each
// card is a Link that navigates to the matching admin page when those
// pages exist. Destinations are best-effort — pages are wired in later
// blocks; in v1 they navigate to placeholder URLs the next page brief
// will replace.

interface QuickAction {
  id: string
  label: string
  description: string
  href: string
  icon: typeof Code2
}

const ACTIONS: QuickAction[] = [
  {
    id: "issue-key",
    label: "Issue API key",
    description: "Provision a tenant-scoped key.",
    href: "/customers/api-keys",
    icon: KeyRound,
  },
  {
    id: "add-tenant",
    label: "Add tenant",
    description: "Create a new customer workspace.",
    href: "/customers/tenants",
    icon: Users,
  },
  {
    id: "test-agent",
    label: "Test an agent",
    description: "Run an agent against a sample payload.",
    href: "/build/agents",
    icon: PlayCircle,
  },
  {
    id: "api-explorer",
    label: "Open API explorer",
    description: "Browse the runtime REST surface.",
    href: "/build/api-explorer",
    icon: Code2,
  },
]

export function QuickActions() {
  return (
    <section
      aria-labelledby="quick-actions-heading"
      className="flex flex-col gap-stack"
    >
      <h2
        id="quick-actions-heading"
        className="text-eyebrow uppercase text-muted-foreground"
      >
        Quick actions
      </h2>
      <div className="grid grid-cols-1 gap-stack sm:grid-cols-2 lg:grid-cols-4">
        {ACTIONS.map((a) => (
          <QuickActionCard key={a.id} action={a} />
        ))}
      </div>
    </section>
  )
}

function QuickActionCard({ action }: { action: QuickAction }) {
  const Icon = action.icon
  return (
    <Link
      href={action.href}
      className="block focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-tile"
    >
      <Card className="h-full transition-colors hover:bg-accent">
        <CardContent className="flex flex-col gap-stack">
          <div className="flex items-center justify-between">
            <Icon className="size-5 text-muted-foreground" aria-hidden />
            <ArrowRight
              className="size-4 text-muted-foreground"
              aria-hidden
            />
          </div>
          <CardTitle className="text-body">{action.label}</CardTitle>
          <CardDescription className="text-meta">
            {action.description}
          </CardDescription>
        </CardContent>
      </Card>
    </Link>
  )
}
