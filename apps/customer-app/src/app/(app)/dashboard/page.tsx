// SPDX-License-Identifier: Apache-2.0

import Link from "next/link"
import {
  ArrowRight,
  CheckCircle2,
  Clock3,
  CreditCardIcon,
  FileQuestion,
  LifeBuoy,
  LockKeyhole,
  MessageSquareText,
  ReceiptText,
  ShieldCheck,
  Wrench,
  type LucideIcon,
} from "lucide-react"

import { GuidedTour } from "@/components/guided-tour"
import { Badge } from "@/components/ui/badge"
import { Button, buttonVariants } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { api, type CostEvent } from "@/lib/api"
import { requireCustomerContext } from "@/lib/session"

type Loaded = {
  recentRequests: CostEvent[]
  requestsToday: number
  plan: string
}

function formatRelativeTime(iso: string): string {
  const time = new Date(iso).getTime()
  if (Number.isNaN(time)) return "recently"
  const diff = Date.now() - time
  const minutes = Math.max(1, Math.round(diff / 60_000))
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.round(hours / 24)
  return `${days}d ago`
}

async function load(tenantId: string): Promise<Loaded> {
  let recentRequests: CostEvent[] = []
  let plan = "free"

  try {
    const recent = await api.costEvents({ tenant: tenantId, limit: 5 })
    recentRequests = recent.events
  } catch {
    recentRequests = []
  }

  try {
    const customer = await api.billing.customer(tenantId)
    plan = customer.plan
  } catch {
    plan = "free"
  }

  const cutoff = new Date()
  cutoff.setHours(0, 0, 0, 0)
  const requestsToday = recentRequests.filter(
    (event) => new Date(event.occurred_at).getTime() >= cutoff.getTime(),
  ).length

  return { recentRequests, requestsToday, plan }
}

const SUPPORT_TOPICS = [
  {
    title: "Billing and refunds",
    description: "Invoice questions, renewal concerns, refunds, and payment checks.",
    icon: ReceiptText,
    href: "/support",
  },
  {
    title: "Account access",
    description: "Login issues, device changes, verification, and account recovery.",
    icon: LockKeyhole,
    href: "/support",
  },
  {
    title: "Product issues",
    description: "Exports, errors, missing data, performance, and troubleshooting.",
    icon: Wrench,
    href: "/support",
  },
]

export default async function DashboardPage() {
  const { session, ctx } = await requireCustomerContext()
  const data = await load(ctx.tenantId)
  const firstName = session.user.name?.split(" ")[0] ?? "there"
  const hasRequests = data.recentRequests.length > 0

  return (
    <div className="flex flex-col gap-6">
      <div className="grid gap-4 lg:grid-cols-[1.35fr_0.65fr]">
        <section className="rounded-lg border bg-card p-6 shadow-sm" data-tour="customer-home-hero">
          <div className="flex flex-col gap-5">
            <div>
              <Badge variant="secondary" className="mb-3 gap-1.5">
                <LifeBuoy className="size-3" />
                Support center
              </Badge>
              <h1 className="max-w-2xl text-2xl font-semibold tracking-tight sm:text-3xl">
                Hi {firstName}, what can we help with?
              </h1>
              <p className="text-muted-foreground mt-2 max-w-2xl text-sm">
                Ask about billing, account access, renewals, or product issues. The assistant keeps
                the conversation simple while checking the right details before it replies.
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Link href="/support" className={buttonVariants()}>
                <MessageSquareText data-icon="inline-start" />
                Start support chat
              </Link>
              <Link href="/requests" className={buttonVariants({ variant: "outline" })}>
                View requests
                <ArrowRight data-icon="inline-end" />
              </Link>
            </div>
          </div>
        </section>

        <Card data-tour="customer-home-status">
          <CardHeader>
            <CardTitle className="text-base">Support status</CardTitle>
            <CardDescription>Your current help center summary.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3">
            <StatusRow
              icon={FileQuestion}
              label="Requests today"
              value={String(data.requestsToday)}
            />
            <StatusRow
              icon={Clock3}
              label="Last activity"
              value={
                hasRequests ? formatRelativeTime(data.recentRequests[0].occurred_at) : "None yet"
              }
            />
            <StatusRow icon={CreditCardIcon} label="Plan" value={data.plan} capitalize />
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-3" data-tour="customer-support-topics">
        {SUPPORT_TOPICS.map((topic) => (
          <Card key={topic.title} className="transition-colors hover:border-primary/40">
            <CardHeader>
              <div className="bg-primary/10 text-primary mb-2 flex size-9 items-center justify-center rounded-md">
                <topic.icon className="size-4" />
              </div>
              <CardTitle className="text-base">{topic.title}</CardTitle>
              <CardDescription>{topic.description}</CardDescription>
            </CardHeader>
            <CardContent>
              <Button
                variant="outline"
                size="sm"
                render={<Link href={topic.href}>Ask about this</Link>}
              />
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="grid gap-4 lg:grid-cols-[1fr_0.9fr]">
        <Card data-tour="customer-recent-requests">
          <CardHeader>
            <CardTitle>Recent support activity</CardTitle>
            <CardDescription>Requests you have asked the assistant to help with.</CardDescription>
          </CardHeader>
          <CardContent>
            {data.recentRequests.length === 0 ? (
              <div className="flex flex-col items-center gap-3 py-8 text-center">
                <div className="bg-muted flex size-10 items-center justify-center rounded-md">
                  <MessageSquareText className="text-muted-foreground size-5" />
                </div>
                <div>
                  <p className="text-sm font-medium">No requests yet</p>
                  <p className="text-muted-foreground text-sm">
                    Start a support chat and your activity will appear here.
                  </p>
                </div>
              </div>
            ) : (
              <div className="grid gap-3">
                {data.recentRequests.map((request) => (
                  <div
                    key={request.id}
                    className="flex items-start justify-between gap-3 rounded-md border p-3"
                  >
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">Support request reviewed</p>
                      <p className="text-muted-foreground text-xs">
                        {request.agent ? "Routed through support checks" : "Assistant response"}
                      </p>
                    </div>
                    <Badge variant="outline" className="shrink-0">
                      {formatRelativeTime(request.occurred_at)}
                    </Badge>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>What the assistant checks</CardTitle>
            <CardDescription>
              The customer view stays simple, but each answer is prepared with guardrails.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3">
            <CheckItem label="Classifies the request before answering" />
            <CheckItem label="Extracts facts and asks for missing evidence" />
            <CheckItem label="Avoids promises that need human review" />
            <CheckItem label="Keeps replies clear and calm" />
          </CardContent>
        </Card>
      </div>

      <GuidedTour
        id="customer-help-center-v1"
        autoStart={!hasRequests}
        steps={[
          {
            element: "[data-tour='customer-home-hero']",
            popover: {
              title: "Start with the customer need",
              description:
                "This is a real support portal. The customer starts with their issue, not setup.",
              side: "bottom",
              align: "start",
            },
          },
          {
            element: "[data-tour='customer-support-topics']",
            popover: {
              title: "Pick a support path",
              description:
                "The visible choices are normal customer problems. The assistant decides which checks are needed after the chat begins.",
              side: "top",
              align: "start",
            },
          },
          {
            element: "[data-tour='customer-recent-requests']",
            popover: {
              title: "Activity becomes support history",
              description:
                "After a chat, the customer sees request history here. The operator can inspect evidence separately.",
              side: "top",
              align: "start",
            },
          },
        ]}
      />
    </div>
  )
}

function StatusRow({
  icon: Icon,
  label,
  value,
  capitalize = false,
}: {
  icon: LucideIcon
  label: string
  value: string
  capitalize?: boolean
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-md border p-3">
      <div className="flex min-w-0 items-center gap-2">
        <Icon className="text-muted-foreground size-4 shrink-0" />
        <span className="text-muted-foreground truncate text-sm">{label}</span>
      </div>
      <span className={capitalize ? "text-sm font-medium capitalize" : "text-sm font-medium"}>
        {value}
      </span>
    </div>
  )
}

function CheckItem({ label }: { label: string }) {
  return (
    <div className="flex items-start gap-3">
      <ShieldCheck className="mt-0.5 size-4 shrink-0 text-emerald-500" />
      <span className="text-sm">{label}</span>
    </div>
  )
}
