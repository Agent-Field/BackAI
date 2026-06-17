// SPDX-License-Identifier: Apache-2.0

"use client"

import * as React from "react"

import { cn } from "@/lib/utils"

// FilterChip — selectable filter pill with an optional count badge.
//
// Why this is its own primitive:
//   - Earlier we composed Button(variant=default) + Badge(variant=secondary)
//     to fake a chip. That works in light mode but inverts badly in dark
//     mode: the active button background becomes white (bg-primary), and
//     the count badge's bg-secondary lands as dark grey — leaving the label
//     itself white-on-white at certain class-merge orderings.
//   - shadcn variants are tuned per-component, not per-composition.
//     Nesting a Badge inside an inverted Button is not a tested pattern.
//   - A dedicated primitive owns BOTH halves (chip + count) of the active
//     state, so the contrast is explicit and auditable in one place.
//
// All colour comes from semantic tokens declared in globals.css. No raw
// hex, no Tailwind variant-default magic, no nested cross-component
// inheritance.

interface FilterChipProps {
  label: string
  count?: number
  active: boolean
  onSelect: () => void
  /** Optional aria-label override; defaults to `${label} filter` */
  ariaLabel?: string
}

export function FilterChip({
  label,
  count,
  active,
  onSelect,
  ariaLabel,
}: FilterChipProps) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      aria-label={ariaLabel ?? `${label} filter`}
      data-state={active ? "on" : "off"}
      onClick={onSelect}
      className={cn(
        // Geometry shared by both states. Tight height + meta font keeps
        // chips visually subordinate to the list rows below — chips are
        // navigation, not content. Keep size + radius here so active and
        // inactive can't accidentally diverge.
        "inline-flex h-6 shrink-0 items-center gap-inline rounded-md border px-pill-x text-meta leading-none transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        active
          ? // Active: invert the surface. text-background reads dark on
            // the white active pill in dark mode and white on the dark
            // active pill in light mode — both intentional.
            "border-foreground bg-foreground text-background hover:bg-foreground/90"
          : // Inactive: quiet outline; only the label colour shifts on
            // hover so the page stays calm.
            "border-border bg-card text-muted-foreground hover:bg-accent hover:text-foreground",
      )}
    >
      <span>{label}</span>
      {count !== undefined ? (
        <span
          aria-hidden
          className={cn(
            // Count chip sits inside the chip. Smaller than the label
            // so it reads as metadata, not as the primary affordance.
            // Active state contrasts against the inverted background.
            "inline-flex h-3.5 min-w-3.5 items-center justify-center rounded-pill px-1 font-mono text-eyebrow tabular-nums leading-none",
            active
              ? // On the inverted active surface: a translucent halo.
                "bg-background/25 text-background"
              : // On the calm inactive surface: quiet muted pill.
                "bg-muted text-muted-foreground",
          )}
        >
          {count}
        </span>
      ) : null}
    </button>
  )
}

interface FilterChipGroupProps {
  label?: string
  children: React.ReactNode
}

export function FilterChipGroup({ label, children }: FilterChipGroupProps) {
  return (
    <div role="radiogroup" className="flex items-center gap-inline">
      {label ? (
        <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
          {label}
        </span>
      ) : null}
      <div className="flex items-center gap-inline">{children}</div>
    </div>
  )
}
