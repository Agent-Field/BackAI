// SPDX-License-Identifier: Apache-2.0

"use client"

// DateTimePicker — shadcn Popover + Calendar + a 24h time input.
//
// Replaces native <input type="datetime-local"> calls that rendered the
// browser's default datepicker (which looks nothing like the rest of
// our shadcn UI). Used by /customers/audit, /build/jobs filters, and
// the api-keys "expires at" field.
//
// Value contract: ISO 8601 string (e.g. "2026-06-07T15:30") or "" when
// unset. We deliberately use the local-without-zone shape since that's
// what every consumer was already passing to URL search params.

import * as React from "react"
import { CalendarIcon, X } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import { Input } from "@/components/ui/input"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { cn } from "@/lib/utils"

type Props = {
  /** ISO-like local string, e.g. "2026-06-07T15:30". "" = unset. */
  value: string
  onChange: (value: string) => void
  placeholder?: string
  disabled?: boolean
  className?: string
  /** Aria label for screen-readers. */
  "aria-label"?: string
  /** Whether to include the time row. Default true. */
  withTime?: boolean
}

function parseISOLocal(s: string): Date | undefined {
  if (!s) return undefined
  // s shape: "YYYY-MM-DDTHH:MM" or "YYYY-MM-DD"
  const t = new Date(s)
  return Number.isNaN(t.getTime()) ? undefined : t
}

function formatLocal(d: Date, withTime: boolean): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, "0")
  const day = String(d.getDate()).padStart(2, "0")
  if (!withTime) return `${y}-${m}-${day}`
  const hh = String(d.getHours()).padStart(2, "0")
  const mm = String(d.getMinutes()).padStart(2, "0")
  return `${y}-${m}-${day}T${hh}:${mm}`
}

function displayLabel(value: string, withTime: boolean): string {
  const d = parseISOLocal(value)
  if (!d) return ""
  if (!withTime) {
    return d.toLocaleDateString(undefined, {
      year: "numeric",
      month: "short",
      day: "numeric",
    })
  }
  return d.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  })
}

export function DateTimePicker({
  value,
  onChange,
  placeholder = "Pick a date",
  disabled,
  className,
  withTime = true,
  ...rest
}: Props) {
  const [open, setOpen] = React.useState(false)
  const current = parseISOLocal(value)
  const label = displayLabel(value, withTime)
  const timeValue = current ? formatLocal(current, true).slice(11, 16) : ""

  const handleDayPick = (day: Date | undefined) => {
    if (!day) {
      onChange("")
      return
    }
    // Preserve current time when changing only the date.
    const next = new Date(day)
    if (current) {
      next.setHours(current.getHours(), current.getMinutes(), 0, 0)
    } else {
      next.setHours(0, 0, 0, 0)
    }
    onChange(formatLocal(next, withTime))
  }

  const handleTimeChange = (t: string) => {
    if (!t) return
    const [hh, mm] = t.split(":").map((n) => parseInt(n, 10))
    if (Number.isNaN(hh) || Number.isNaN(mm)) return
    const base = current ?? new Date()
    base.setHours(hh, mm, 0, 0)
    onChange(formatLocal(base, withTime))
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            variant="outline"
            disabled={disabled}
            aria-label={rest["aria-label"]}
            className={cn(
              "justify-start font-normal",
              !value && "text-muted-foreground",
              className,
            )}
          >
            <CalendarIcon data-icon="inline-start" />
            <span className="flex-1 truncate text-left">
              {label || placeholder}
            </span>
            {value ? (
              <button
                type="button"
                aria-label="Clear"
                className="hover:text-foreground text-muted-foreground -mr-1 inline-flex items-center"
                onClick={(e) => {
                  e.stopPropagation()
                  onChange("")
                }}
              >
                <X className="size-3.5" />
              </button>
            ) : null}
          </Button>
        }
      />
      <PopoverContent align="start" className="w-auto p-0">
        <Calendar
          mode="single"
          selected={current}
          onSelect={handleDayPick}
          autoFocus
        />
        {withTime ? (
          <div className="border-t p-3">
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground text-xs">Time</span>
              <Input
                type="time"
                step={60}
                value={timeValue}
                onChange={(e) => handleTimeChange(e.target.value)}
                className="h-8 w-32"
              />
            </div>
          </div>
        ) : null}
      </PopoverContent>
    </Popover>
  )
}
