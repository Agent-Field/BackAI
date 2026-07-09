// SPDX-License-Identifier: Apache-2.0

import type { FeatureFlag } from "@/lib/api"

// Presentation helpers for the feature-flags list. Kept out of the
// components so the source vocabulary and ordering have one home.

// The runtime tags every flag with where its current value came from:
//   - "default": the built-in value shipped in code, never overridden.
//   - "db":      an operator override persisted in the flag store.
// Anything else is passed through verbatim (forward-compatible with
// future sources like "env").
export function sourceLabel(source: string): string {
  switch (source) {
    case "default":
      return "built-in default"
    case "db":
      return "operator override"
    default:
      return source
  }
}

// A flag whose current value is a persisted override reads as "changed"
// from its shipped default — worth calling out so operators can spot
// non-standard configuration at a glance.
export function isOverridden(flag: FeatureFlag): boolean {
  return flag.source === "db"
}

// Enabled flags first (they're the ones actively changing behaviour),
// then alphabetical by label so the list is stable across refreshes.
export function sortFlags(flags: FeatureFlag[]): FeatureFlag[] {
  return flags
    .slice()
    .sort(
      (a, b) =>
        Number(b.enabled) - Number(a.enabled) ||
        a.label.localeCompare(b.label) ||
        a.key.localeCompare(b.key),
    )
}
