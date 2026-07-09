// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import { toast } from "sonner"

import { Switch } from "@/components/ui/switch"

import { api, ApiError } from "@/lib/api"
import type { FeatureFlag } from "@/lib/api"

import { isOverridden, sourceLabel } from "@/lib/flags-page/derive"

// One row per flag: label + description on the left, a source note and
// last-changed timestamp underneath, and an enable/disable Switch on the
// right. The Switch drives a PUT via api.config.flags.set; the shell owns
// the snapshot, so on success we hand it the flag the runtime echoed back
// (authoritative source/updated_at) rather than optimistically guessing.
// If the runtime has no flag store the write returns 503 — we surface that
// as a plain-language toast instead of a raw error.

interface FlagRowProps {
  flag: FeatureFlag
  onChanged: (flag: FeatureFlag) => void
}

export function FlagRow({ flag, onChanged }: FlagRowProps) {
  const [busy, setBusy] = useState(false)

  const toggle = async (next: boolean) => {
    setBusy(true)
    try {
      const updated = await api.config.flags.set(flag.key, { enabled: next })
      onChanged(updated)
      toast.success(`${flag.label} ${next ? "enabled" : "disabled"}`)
    } catch (err) {
      if (err instanceof ApiError && err.code === "FEATURE_FLAGS_NOT_CONFIGURED") {
        toast.error("Feature-flag persistence is not configured", {
          description:
            "This runtime has no flag database, so overrides can't be saved. Attach Postgres and restart to enable toggles.",
        })
      } else {
        toast.error("Could not update the flag", {
          description: err instanceof Error ? err.message : String(err),
        })
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <li className="flex items-start justify-between gap-stack px-row-x py-row-y">
      <div className="flex min-w-0 flex-col gap-tile-tight">
        <span className="flex items-center gap-tile-tight">
          <span className="truncate text-body font-medium text-foreground">{flag.label}</span>
          <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-meta text-muted-foreground">
            {flag.key}
          </code>
        </span>
        <span className="max-w-prose text-meta text-muted-foreground">{flag.description}</span>
        <span className="flex items-center gap-tile-tight text-eyebrow uppercase text-muted-foreground">
          <span>{sourceLabel(flag.source)}</span>
          {isOverridden(flag) && flag.updated_at ? (
            <>
              <span aria-hidden>·</span>
              <span className="tabular-nums normal-case">changed {ageLabel(flag.updated_at)}</span>
            </>
          ) : null}
        </span>
      </div>

      <Switch
        checked={flag.enabled}
        disabled={busy}
        onCheckedChange={toggle}
        aria-label={`${flag.enabled ? "Disable" : "Enable"} ${flag.label}`}
      />
    </li>
  )
}

function ageLabel(timestamp: string): string {
  const parsed = Date.parse(timestamp)
  if (Number.isNaN(parsed)) return "recently"
  const ageSec = Math.max(0, Math.round((Date.now() - parsed) / 1000))
  if (ageSec < 60) return "just now"
  const ageMin = Math.round(ageSec / 60)
  if (ageMin < 60) return `${ageMin}m ago`
  const ageHr = Math.round(ageMin / 60)
  if (ageHr < 24) return `${ageHr}h ago`
  return `${Math.round(ageHr / 24)}d ago`
}
