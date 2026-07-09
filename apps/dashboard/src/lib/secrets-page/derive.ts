// SPDX-License-Identifier: Apache-2.0

// Pure helpers for the secrets surface. Kept out of data.ts so client
// components can import them (data.ts is server-only).

// Secret keys mirror the runtime's validateKey(): non-empty, ≤256 chars,
// and restricted to [A-Za-z0-9_.-/]. Validating client-side lets the set
// dialog reject bad keys before a round-trip.
const SECRET_KEY_RE = /^[A-Za-z0-9_./-]+$/

export function validateSecretKey(key: string): string | null {
  const trimmed = key.trim()
  if (!trimmed) return "Key is required."
  if (trimmed.length > 256) return "Key must be 256 characters or fewer."
  if (!SECRET_KEY_RE.test(trimmed)) return "Key may only contain letters, digits, and _ . - /"
  return null
}

// Rotation schedule derived from the (nullable) RFC3339 rotate_after
// deadline. `overdue` drives a warning badge once the deadline has
// passed; `label` is a short human phrase for the table cell.
export interface RotationStatus {
  scheduled: boolean
  overdue: boolean
  label: string
}

export function rotationStatus(rotateAfter: string | null): RotationStatus {
  if (!rotateAfter) {
    return { scheduled: false, overdue: false, label: "—" }
  }
  const ts = Date.parse(rotateAfter)
  if (Number.isNaN(ts)) {
    return { scheduled: true, overdue: false, label: rotateAfter }
  }
  const diffMs = ts - Date.now()
  if (diffMs <= 0) {
    return { scheduled: true, overdue: true, label: `overdue ${relative(-diffMs)}` }
  }
  return { scheduled: true, overdue: false, label: `in ${relative(diffMs)}` }
}

// Compact magnitude label (e.g. "3d", "5h") for a positive duration.
function relative(ms: number): string {
  const sec = Math.floor(ms / 1000)
  if (sec < 60) return `${sec}s`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr}h`
  return `${Math.floor(hr / 24)}d`
}

// datetime-local input value (no timezone) → RFC3339 string the runtime
// can parse, or undefined when the field is left blank.
export function toRotateAfterISO(datetimeLocal: string): string | undefined {
  const trimmed = datetimeLocal.trim()
  if (!trimmed) return undefined
  const ts = Date.parse(trimmed)
  if (Number.isNaN(ts)) return undefined
  return new Date(ts).toISOString()
}

// RFC3339 rotate_after → the value shape a datetime-local input expects
// ("YYYY-MM-DDTHH:mm"), so editing a secret pre-fills its current
// schedule. Empty when there is no deadline.
export function toDatetimeLocal(rotateAfter: string | null): string {
  if (!rotateAfter) return ""
  const ts = Date.parse(rotateAfter)
  if (Number.isNaN(ts)) return ""
  const d = new Date(ts)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}`
}
