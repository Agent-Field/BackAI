// SPDX-License-Identifier: Apache-2.0

"use client"

import * as React from "react"
import Link from "next/link"
import {
  CheckCircle2,
  Circle,
  ExternalLink,
  KeyRound,
  Landmark,
  Split,
  WalletCards,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

const STORAGE_KEY = "af-stack:getting-started:customer-app-opened"

export type GettingStartedState = {
  hasTenant: boolean
  hasApiKey: boolean
  hasBudget: boolean
}

type Step = {
  id: string
  label: string
  description: string
  done: boolean
  icon: typeof Split
  action: React.ReactNode
}

export function GettingStartedPanel({
  state,
  customerAppUrl,
}: {
  state: GettingStartedState
  customerAppUrl: string
}) {
  const [openedCustomerApp, setOpenedCustomerApp] = React.useState(false)

  React.useEffect(() => {
    setOpenedCustomerApp(window.localStorage.getItem(STORAGE_KEY) === "true")
  }, [])

  const steps: Step[] = [
    {
      id: "tenant",
      label: "Create tenant",
      description: "Start tenant-scoped data, secrets, and quotas.",
      done: state.hasTenant,
      icon: Split,
      action: (
        <Button
          size="sm"
          variant="outline"
          nativeButton={false}
          render={<Link href="/customers/tenants">Tenants</Link>}
        />
      ),
    },
    {
      id: "api-key",
      label: "Issue API key",
      description: "Give your app or customer a scoped runtime key.",
      done: state.hasApiKey,
      icon: KeyRound,
      action: (
        <Button
          size="sm"
          variant="outline"
          nativeButton={false}
          render={<Link href="/customers/api-keys">API keys</Link>}
        />
      ),
    },
    {
      id: "budget",
      label: "Set budget",
      description: "Add a monthly LLM spend guardrail.",
      done: state.hasBudget,
      icon: WalletCards,
      action: (
        <Button
          size="sm"
          variant="outline"
          nativeButton={false}
          render={<Link href="/operate/cost">Cost</Link>}
        />
      ),
    },
    {
      id: "customer-app",
      label: "Open customer app",
      description: "Check the customer-facing side of your fork.",
      done: openedCustomerApp,
      icon: Landmark,
      action: (
        <Button
          size="sm"
          variant="outline"
          nativeButton={false}
          render={
            <a
              href={customerAppUrl}
              target="_blank"
              rel="noreferrer"
              onClick={() => {
                window.localStorage.setItem(STORAGE_KEY, "true")
                setOpenedCustomerApp(true)
              }}
            >
              Open
              <ExternalLink className="size-3.5" />
            </a>
          }
        />
      ),
    },
  ]

  if (steps.every((step) => step.done)) {
    return null
  }

  return (
    <Card className="border-dashed">
      <CardHeader>
        <CardTitle>Getting started</CardTitle>
        <CardDescription>
          Finish these first-run steps to turn the fresh stack into a usable backend.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          {steps.map((step) => {
            const Icon = step.icon
            const StatusIcon = step.done ? CheckCircle2 : Circle
            return (
              <div
                key={step.id}
                className="flex min-h-36 flex-col justify-between rounded-md border p-4"
              >
                <div className="space-y-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="bg-muted flex size-8 items-center justify-center rounded-md">
                      <Icon className="size-4" />
                    </div>
                    <StatusIcon
                      className={step.done ? "text-primary size-4" : "text-muted-foreground size-4"}
                    />
                  </div>
                  <div>
                    <div className="font-medium">{step.label}</div>
                    <p className="text-muted-foreground mt-1 text-sm">{step.description}</p>
                  </div>
                </div>
                <div className="pt-4">{step.action}</div>
              </div>
            )
          })}
        </div>
      </CardContent>
    </Card>
  )
}
