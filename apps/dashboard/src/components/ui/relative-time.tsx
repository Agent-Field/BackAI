// SPDX-License-Identifier: Apache-2.0

"use client"

import { useEffect, useState } from "react"

// Shared relative-time display. Fixes React #418 ("Text content does not
// match") that this dashboard hit in ~13 places: the old pattern read
// Date.now() DURING render inside "use client" shells, so the SSR snapshot
// and the client hydration a moment later produced different "Ns ago"
// strings whenever a value fell in a seconds bucket. Here the
// clock-dependent label is only computed AFTER hydration (inside a mounted
// effect), so SSR and the first client paint are byte-identical and the
// live label appears identically on every client.

interface RelativeTimeProps {
  /** ISO timestamp to describe. null/undefined renders the "—" placeholder. */
  iso: string | null | undefined
  /** Passed straight through to the rendered <time> element. */
  className?: string
  /**
   * Bucketing override — pass a page's existing formatter when its
   * bucketing differs (e.g. crons render "in Xm" for future ticks).
   * Defaults to the canonical formatRelative. Must be a stable reference
   * (a module-level function) so the refresh interval isn't torn down and
   * rebuilt on every parent render.
   */
  format?: (iso: string | null) => string
  /** How often to refresh the live label once mounted. Default 30s. */
  intervalMs?: number
}

// Canonical relative-time formatter — the logic that had been copy-pasted
// into ~13 derive.ts files. Buckets: sub-second → "now", <1m → "Ns ago",
// <1h → "Nm ago", <1d → "Nh ago", older → "MM-DD". "—" for null; echoes
// the raw string when it cannot be parsed.
export function formatRelative(iso: string | null): string {
  if (!iso) return "—"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = Math.max(0, Date.now() - ts)
  if (diffMs < 1000) return "now"
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86_400) return `${Math.floor(sec / 3600)}h ago`
  return new Date(ts).toISOString().slice(5, 10)
}

// Clock-free first-paint text. This MUST be a pure function of `iso` (no
// Date.now()) so the server render and the first client render agree —
// that agreement is what prevents the hydration mismatch. We show the
// absolute UTC date/time (timezone-stable via toISOString, so it can't
// diverge between a UTC server and a local-timezone client); the mounted
// effect then swaps in the live relative label.
function staticLabel(iso: string | null | undefined): string {
  if (!iso) return "—"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  return new Date(ts).toISOString().slice(5, 16).replace("T", " ")
}

export function RelativeTime({
  iso,
  className,
  format = formatRelative,
  intervalMs = 30_000,
}: RelativeTimeProps) {
  // Seed with the clock-free placeholder so SSR === first client paint.
  const [label, setLabel] = useState<string>(() => staticLabel(iso))

  useEffect(() => {
    // Normalize undefined → null so the (string | null) formatters apply.
    const tick = () => setLabel(format(iso ?? null))
    // Post-hydration: safe to read the clock now. Every client runs this
    // identically, so no two renders can disagree on the text.
    tick()
    const id = setInterval(tick, intervalMs)
    return () => clearInterval(id)
  }, [iso, format, intervalMs])

  return (
    <time className={className} dateTime={iso ?? undefined} suppressHydrationWarning>
      {label}
    </time>
  )
}
