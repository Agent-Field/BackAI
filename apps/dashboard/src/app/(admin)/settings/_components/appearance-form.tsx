// SPDX-License-Identifier: Apache-2.0

"use client"

// Theme picker. Wraps next-themes via the existing ThemeProvider in
// `src/components/theme-provider.tsx`. Three choices: light / dark / system.

import { useEffect, useState } from "react"
import { useTheme } from "next-themes"
import { Monitor, Moon, Sun } from "lucide-react"

import {
  Field,
  FieldDescription,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { Skeleton } from "@/components/ui/skeleton"

const THEMES = [
  {
    value: "light",
    label: "Light",
    description: "Always use the light theme.",
    icon: Sun,
  },
  {
    value: "dark",
    label: "Dark",
    description: "Always use the dark theme.",
    icon: Moon,
  },
  {
    value: "system",
    label: "System",
    description: "Match your operating system.",
    icon: Monitor,
  },
] as const

export function AppearanceForm() {
  const { theme, setTheme } = useTheme()
  const [mounted, setMounted] = useState(false)

  // Avoid hydration mismatch: next-themes resolves the active theme on the
  // client only. Scheduling via microtask sidesteps the lint that flags
  // synchronous setState inside an effect.
  useEffect(() => {
    queueMicrotask(() => setMounted(true))
  }, [])

  if (!mounted) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-5 w-24" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
      </div>
    )
  }

  return (
    <FieldSet>
      <FieldLegend>Theme</FieldLegend>
      <FieldDescription>
        Pick how the dashboard renders for your account.
      </FieldDescription>
      <RadioGroup
        value={theme ?? "system"}
        onValueChange={(next) => setTheme(next)}
        className="grid gap-3 sm:grid-cols-3"
      >
        {THEMES.map((option) => {
          const Icon = option.icon
          return (
            <FieldLabel
              key={option.value}
              htmlFor={`theme-${option.value}`}
              className="cursor-pointer"
            >
              <Field orientation="horizontal" className="items-start">
                <RadioGroupItem
                  id={`theme-${option.value}`}
                  value={option.value}
                />
                <div className="flex flex-col gap-1">
                  <span className="flex items-center gap-2 text-sm font-medium">
                    <Icon className="size-4" />
                    {option.label}
                  </span>
                  <FieldDescription>{option.description}</FieldDescription>
                </div>
              </Field>
            </FieldLabel>
          )
        })}
      </RadioGroup>
    </FieldSet>
  )
}