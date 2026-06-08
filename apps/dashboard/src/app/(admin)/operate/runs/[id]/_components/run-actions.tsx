// SPDX-License-Identifier: Apache-2.0

// run-actions.tsx — #25 control verbs for an AgentField run.
//
// Buttons proxy to the runtime's `POST /api/v1/runs/{id}/{verb}` routes,
// which in turn proxy to AgentField's agent-api surface. The "View in
// AgentField" anchor opens AgentField's UI in a new tab for the deep
// DAG + step inspector.
//
// `actionsAvailable` is a server-computed allowlist: buttons gray out
// when their verb isn't valid for the current run state. We never block
// submission client-side; AgentField is the authority — this is a UX
// hint, not a security gate.

"use client"

import * as React from "react"
import {
  CheckCheck,
  ExternalLink,
  Pause,
  Play,
  XCircle,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import { api, ApiError } from "@/lib/api"

type RunActionsProps = {
  runId: string
  actionsAvailable: string[]
  agentfieldDetailsUrl: string
  onActionComplete?: () => void
}

type Verb = "cancel" | "pause" | "resume" | "request-approval"

const verbLabels: Record<Verb, string> = {
  cancel: "Cancel",
  pause: "Pause",
  resume: "Resume",
  "request-approval": "Request approval",
}

const verbIcons: Record<Verb, React.ReactNode> = {
  cancel: <XCircle className="size-4" />,
  pause: <Pause className="size-4" />,
  resume: <Play className="size-4" />,
  "request-approval": <CheckCheck className="size-4" />,
}

export function RunActions({
  runId,
  actionsAvailable,
  agentfieldDetailsUrl,
  onActionComplete,
}: RunActionsProps) {
  const [pendingVerb, setPendingVerb] = React.useState<Verb | null>(null)
  const [error, setError] = React.useState<string | null>(null)
  const allowed = React.useMemo(
    () => new Set(actionsAvailable),
    [actionsAvailable],
  )

  const handle = async (verb: Verb) => {
    setPendingVerb(verb)
    setError(null)
    try {
      switch (verb) {
        case "cancel":
          await api.runActions.cancel(runId)
          break
        case "pause":
          await api.runActions.pause(runId)
          break
        case "resume":
          await api.runActions.resume(runId)
          break
        case "request-approval":
          await api.runActions.requestApproval(runId)
          break
      }
      onActionComplete?.()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Action failed"
      setError(message)
    } finally {
      setPendingVerb(null)
    }
  }

  const verbs: Verb[] = ["cancel", "pause", "resume", "request-approval"]

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        {verbs.map((v) => {
          const enabled = allowed.has(v) && pendingVerb === null
          return (
            <Button
              key={v}
              size="sm"
              variant={v === "cancel" ? "destructive" : "outline"}
              disabled={!enabled}
              onClick={() => void handle(v)}
            >
              {verbIcons[v]}
              {verbLabels[v]}
              {pendingVerb === v ? "…" : ""}
            </Button>
          )
        })}
        <Button
          size="sm"
          variant="outline"
          render={
            <a
              href={agentfieldDetailsUrl}
              target="_blank"
              rel="noopener noreferrer"
            >
              View in AgentField
              <ExternalLink className="size-4" />
            </a>
          }
        />
      </div>
      {error ? (
        <p className="text-destructive text-xs" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  )
}
