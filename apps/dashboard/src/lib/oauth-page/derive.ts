// SPDX-License-Identifier: Apache-2.0

import type { OAuthConnection } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"

// Status helpers ---------------------------------------------------------

// Connection status is a free-form wire string. Collapse the known words
// to the four-tone dashboard vocabulary; anything unrecognised reads idle
// rather than inventing a colour.
export function classifyConnectionStatus(status?: string | null): StatusState {
  switch ((status ?? "").toLowerCase()) {
    case "active":
    case "connected":
    case "valid":
      return "ok"
    case "expiring":
    case "stale":
      return "watch"
    case "expired":
    case "revoked":
    case "error":
    case "invalid":
      return "act"
    default:
      return "idle"
  }
}

// Refresh attempts are pass/fail with a "pending" in-flight state. Map to
// the tones the history table paints.
export function classifyRefreshStatus(status: string): StatusState {
  switch (status.toLowerCase()) {
    case "success":
    case "succeeded":
    case "ok":
    case "refreshed":
      return "ok"
    case "failed":
    case "error":
    case "revoked":
      return "act"
    case "pending":
    case "in_progress":
    case "retrying":
      return "watch"
    default:
      return "idle"
  }
}

// Connection lookups -----------------------------------------------------

// A provider is "connected" when at least one non-revoked connection
// exists for it. Providers advertise capability; connections prove one is
// live.
export function connectionForProvider(
  provider: string,
  connections: OAuthConnection[],
): OAuthConnection | undefined {
  return connections.find(
    (c) => c.provider === provider && classifyConnectionStatus(c.status) !== "act",
  )
}

export function isProviderConnected(provider: string, connections: OAuthConnection[]): boolean {
  return connectionForProvider(provider, connections) !== undefined
}

// authorize() returns an opaque record — the runtime hasn't committed to a
// single key name across providers. Probe the common ones and hand back
// the first string that looks like a redirect target.
const AUTHORIZE_URL_KEYS = [
  "authorize_url",
  "authorization_url",
  "auth_url",
  "redirect_url",
  "consent_url",
  "url",
  "location",
]

export function extractAuthorizeUrl(record: Record<string, unknown>): string | null {
  for (const key of AUTHORIZE_URL_KEYS) {
    const value = record[key]
    if (typeof value === "string" && /^https?:\/\//i.test(value)) return value
  }
  return null
}

// Formatters -------------------------------------------------------------

export function providerLabel(provider: string): string {
  return provider
    .split(/[_\-\s]+/)
    .map((word) =>
      word.length <= 3 ? word.toUpperCase() : word.charAt(0).toUpperCase() + word.slice(1),
    )
    .join(" ")
}

export function formatOAuthAge(iso: string | null | undefined): string {
  if (!iso) return "—"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = Math.max(0, Date.now() - ts)
  if (diffMs < 1000) return "now"
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86_400) return `${Math.floor(sec / 3600)}h ago`
  return new Date(ts).toISOString().slice(5, 16).replace("T", " ")
}

// Token expiry reads forward, not backward: "in 42m" / "expired".
export function formatExpiry(iso: string | null | undefined): string {
  if (!iso) return "no expiry"
  const ts = Date.parse(iso)
  if (Number.isNaN(ts)) return iso
  const diffMs = ts - Date.now()
  if (diffMs <= 0) return "expired"
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return "in <1m"
  if (sec < 3600) return `in ${Math.floor(sec / 60)}m`
  if (sec < 86_400) return `in ${Math.floor(sec / 3600)}h`
  return `in ${Math.floor(sec / 86_400)}d`
}

export function shortId(id: string): string {
  return id.length > 12 ? id.slice(0, 12) : id
}
