// SPDX-License-Identifier: Apache-2.0

import Link from "next/link"
import { CheckCircle2, Circle, ExternalLink, Send } from "lucide-react"

import { buttonVariants } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

type Props = {
  tenantId: string
  hasCalls: boolean
}

function operatorCostUrl(tenantId: string): string {
  const base = process.env.NEXT_PUBLIC_OPERATOR_URL ?? "http://localhost:33000"
  return `${base}/operate/cost?tenant=${encodeURIComponent(tenantId)}`
}

export function FirstRunPanel({ tenantId, hasCalls }: Props) {
  const steps = [
    {
      label: "Account",
      detail: "Tenant, membership, billing record, and API key are provisioned.",
      done: true,
    },
    {
      label: "Support reply",
      detail: hasCalls
        ? "A model call has been billed to this tenant."
        : "Draft one reply to create a cost event.",
      done: hasCalls,
    },
    {
      label: "Admin evidence",
      detail: hasCalls
        ? "Usage is ready to inspect in the operator console."
        : "The exact request link appears after a reply is drafted.",
      done: hasCalls,
    },
  ]

  const stackTags = [
    "Postgres tenancy",
    "better-auth session",
    "LLM gateway",
    "usage ledger",
    "agent runtime",
  ]

  return (
    <Card data-tour="customer-first-run">
      <CardHeader>
        <CardTitle>First run</CardTitle>
        <CardDescription>
          Create one support action and inspect the platform record behind it.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <ol className="grid gap-3 md:grid-cols-3">
          {steps.map((step) => {
            const Icon = step.done ? CheckCircle2 : Circle
            return (
              <li key={step.label} className="flex gap-3">
                <Icon
                  className={
                    step.done
                      ? "mt-0.5 size-4 shrink-0 text-emerald-500"
                      : "text-muted-foreground mt-0.5 size-4 shrink-0"
                  }
                  aria-hidden
                />
                <div>
                  <div className="text-sm font-medium">{step.label}</div>
                  <p className="text-muted-foreground text-sm">{step.detail}</p>
                </div>
              </li>
            )
          })}
        </ol>
        <div
          className="flex flex-wrap gap-1.5 pt-1"
          data-tour="customer-stack-tags"
          aria-label="Backend services used by the first run"
        >
          {stackTags.map((tag) => (
            <span
              key={tag}
              className="rounded-md border bg-muted/40 px-2 py-1 text-xs text-muted-foreground"
            >
              {tag}
            </span>
          ))}
        </div>
        <div className="flex flex-wrap gap-2">
          <Link href="/code-helper" className={buttonVariants()} data-tour="customer-draft-action">
            <Send data-icon="inline-start" />
            Draft reply
          </Link>
          <Link
            href={operatorCostUrl(tenantId)}
            target="_blank"
            className={buttonVariants({ variant: "outline" })}
          >
            <ExternalLink data-icon="inline-start" />
            Open admin cost view
          </Link>
        </div>
      </CardContent>
    </Card>
  )
}
