// SPDX-License-Identifier: Apache-2.0

// Pure helpers shared by the Activity page's server page + client rows.
// No "server-only" here — client components import these too.

export function parseOffset(raw: string | undefined): number {
  const n = Number.parseInt(raw ?? "", 10)
  if (Number.isNaN(n) || n < 0) return 0
  return n
}

// Local wall-clock timestamp. Rendered inside a suppressHydrationWarning
// <time> because the server's timezone can differ from the operator's.
export function formatWhen(iso: string): string {
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  return new Date(ts).toLocaleString(undefined, {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  })
}

export function shortId(id: string): string {
  return id.length > 12 ? id.slice(0, 12) : id
}

export function truncateUserAgent(ua: string, max = 48): string {
  return ua.length > max ? `${ua.slice(0, max - 1)}…` : ua
}
