// SPDX-License-Identifier: Apache-2.0

"use client"

// Feature flags — placeholder UI.
//
// TODO(Phase 5): Wire to runtime config. Today's toggles are no-op locally so
// operators can preview the shape; the runtime is the source of truth. Once
// the config API lands, replace `useState` with a fetch-and-PATCH against
// `api.modulesState` (or a dedicated endpoint), and surface server errors via
// `toast.error`.

import { useState } from "react"

import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Switch } from "@/components/ui/switch"

type Flag = {
  id: string
  label: string
  description: string
  defaultValue: boolean
}

const FLAGS: Flag[] = [
  {
    id: "experimental-cost-forecasts",
    label: "Experimental cost forecasts",
    description:
      "Show 30-day spend forecasts on the Cost page based on rolling averages.",
    defaultValue: false,
  },
  {
    id: "command-palette-recents",
    label: "Command palette recents",
    description:
      "Remember the last few destinations and show them at the top of ⌘K.",
    defaultValue: true,
  },
  {
    id: "verbose-run-logs",
    label: "Verbose run logs",
    description:
      "Include tool input/output payloads in the inline run log viewer.",
    defaultValue: false,
  },
]

export function FeatureFlagsForm() {
  // Local-only state. Runtime persistence wired in Phase 5 (see TODO above).
  const [values, setValues] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(FLAGS.map((f) => [f.id, f.defaultValue])),
  )

  return (
    <FieldGroup>
      {FLAGS.map((flag) => (
        <Field key={flag.id} orientation="horizontal">
          <div className="flex flex-1 flex-col gap-0.5">
            <FieldLabel htmlFor={flag.id}>{flag.label}</FieldLabel>
            <FieldDescription>{flag.description}</FieldDescription>
          </div>
          <Switch
            id={flag.id}
            checked={values[flag.id]}
            onCheckedChange={(next) =>
              setValues((prev) => ({ ...prev, [flag.id]: next }))
            }
          />
        </Field>
      ))}
    </FieldGroup>
  )
}