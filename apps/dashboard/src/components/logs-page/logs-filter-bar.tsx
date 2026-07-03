// SPDX-License-Identifier: Apache-2.0

"use client"

import { Search, X } from "lucide-react"

import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

import type { LogLevelFilter, LogsFilters } from "@/lib/logs-page/types"
import { isLogLevelFilter } from "@/lib/logs-page/types"

// Sticky filter bar for the log stream. Level is an enum select;
// service and search are free-text inputs. All three persist in the
// URL (the shell owns that) and are applied server-side by the
// runtime's log store.

const LEVEL_OPTIONS: { value: LogLevelFilter; label: string }[] = [
  { value: "all", label: "All levels" },
  { value: "debug", label: "Debug" },
  { value: "info", label: "Info" },
  { value: "warn", label: "Warn" },
  { value: "error", label: "Error" },
]

const LEVEL_LABELS: Record<LogLevelFilter, string> = {
  all: "All levels",
  debug: "Debug",
  info: "Info",
  warn: "Warn",
  error: "Error",
}

interface LogsFilterBarProps {
  filters: LogsFilters
  onChange: (next: Partial<LogsFilters>) => void
}

export function LogsFilterBar({ filters, onChange }: LogsFilterBarProps) {
  return (
    <div className="sticky top-12 z-20 flex flex-col gap-stack rounded-md border bg-card/95 px-row-x py-row-y backdrop-blur supports-[backdrop-filter]:bg-card/80">
      <div className="flex flex-wrap items-center gap-stack">
        <div className="flex items-center gap-inline">
          <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
            Level
          </span>
          <Select
            value={filters.level}
            onValueChange={(value) => {
              const raw = typeof value === "string" ? value : "all"
              onChange({ level: isLogLevelFilter(raw) ? raw : "all" })
            }}
          >
            <SelectTrigger size="sm" aria-label="Level filter" className="text-meta">
              <SelectValue>
                {(value: unknown) =>
                  LEVEL_LABELS[
                    typeof value === "string" && isLogLevelFilter(value)
                      ? value
                      : "all"
                  ]
                }
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {LEVEL_OPTIONS.map((opt) => (
                <SelectItem key={opt.value} value={opt.value}>
                  {opt.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex items-center gap-inline">
          <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
            Service
          </span>
          <Input
            type="text"
            value={filters.service}
            placeholder="e.g. runtime"
            onChange={(e) => onChange({ service: e.target.value })}
            className="h-7 w-36 text-meta"
          />
        </div>
        <div className="ml-auto flex min-w-0 flex-1 items-center gap-inline">
          <div className="relative w-full max-w-sm">
            <Search
              className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
              aria-hidden
            />
            <Input
              type="search"
              value={filters.search}
              placeholder="Search message text…"
              onChange={(e) => onChange({ search: e.target.value })}
              className="h-7 pl-7 pr-7 text-meta"
            />
            {filters.search ? (
              <button
                type="button"
                aria-label="Clear search"
                onClick={() => onChange({ search: "" })}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="size-3.5" aria-hidden />
              </button>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  )
}
