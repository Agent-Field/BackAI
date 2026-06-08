// SPDX-License-Identifier: Apache-2.0

// Plugin loader.
//
// A plugin is a contributor-owned folder under `apps/dashboard/plugins/<id>/`
// that exports a `definePlugin({...})` call as its default export from
// `plugin.ts`. A `page.tsx` next to it becomes the plugin's page, auto-mounted
// at `/plugins/<id>`.
//
// The build step `pnpm prebuild` runs `scripts/generate-plugins-manifest.mjs`
// which scans `plugins/*/plugin.ts` and writes `src/lib/plugins.generated.ts`
// (and the Next.js route proxy files under `src/app/(admin)/plugins/<id>/`).
//
// Adding a plugin requires no edits to shell code — sidebar and command
// palette merge plugin nav items into the right group at render time via
// `getNavGroupsWithPlugins()` in `src/lib/nav.ts`.
//
// If `plugins.generated.ts` does not exist (fresh clone, no plugins yet)
// `loadPlugins()` returns an empty array — every consumer must handle the
// empty case.
import type { LucideIcon } from "lucide-react"

// Phase 12.3 — the runtime exposes a Plugin manifest schema via
// `api.plugins()` (see `apps/dashboard/src/lib/api.ts:PluginSchema`).
// That wire-shape uses string icon names (resolved at render time via
// `lucide-react`) and string group names ("Build" | "Operate" |
// "Customers" | "Plugins"). The dashboard's *internal* PluginDefinition
// uses richer types (LucideIcon component, lowercase group ids) because
// the sidebar imports concrete React components.
//
// The scanner emits BOTH:
//   - `plugins.generated.ts`           → PluginDefinition[] (used by nav)
//   - `plugins-manifest.generated.ts`  → Plugin[] in the api.ts shape
//
// `loadPluginManifest()` reads the second file; the runtime endpoint
// returns an empty list and the dashboard merges this constant client-
// side. See `docs/dashboard-plugins.md`.

export type PluginGroup = "build" | "operate" | "customers" | "system"

// Map sidebar group ids to the title-cased group names used in the
// public Plugin manifest (api.ts PluginSchema). "system" plugins map to
// "Plugins" — the new sidebar section reserved for plugins.
export const PLUGIN_GROUP_LABELS: Record<PluginGroup, string> = {
  build: "Build",
  operate: "Operate",
  customers: "Customers",
  system: "Plugins",
}

export type PluginDefinition = {
  /** Stable id, must be unique. Matches the folder name under `plugins/`. */
  id: string
  /** Sidebar + palette label. */
  label: string
  /**
   * Display name used in the public Plugin manifest. Defaults to `label`
   * if unset. Kept as a separate field so the sidebar can render a short
   * label while the public manifest carries the proper product name.
   */
  name?: string
  /**
   * Route the plugin's nav item points at. Defaults to `/plugins/<id>`.
   * The generated route proxy always mounts at `/plugins/<id>` — set this
   * only if your plugin renders its own page under a different path.
   */
  href?: string
  /** Lucide icon component (used by the sidebar). */
  icon: LucideIcon
  /**
   * Lucide icon name as a string (e.g. `"TrendingUp"`). Required so the
   * scanner can include it in the public Plugin manifest — the wire
   * shape uses string icon names, resolved by `lucide-react` at render.
   * Should match the imported icon component for consistency.
   */
  iconName: string
  /** Description used in the ⌘K command palette and Plugin manifest. */
  description?: string
  /** Which sidebar group the nav item joins. */
  group: PluginGroup
  /**
   * Semantic version string, surfaced in the Plugin manifest. Defaults
   * to `"1.0.0"` if unset.
   */
  version?: string
}

/**
 * Define a plugin manifest. Use as the default export from
 * `apps/dashboard/plugins/<id>/plugin.ts`.
 *
 * ```ts
 * import { Sparkles } from "lucide-react"
 * import { definePlugin } from "@/lib/plugins"
 *
 * export default definePlugin({
 *   id: "hello",
 *   label: "Hello",
 *   icon: Sparkles,
 *   iconName: "Sparkles",
 *   description: "Example plugin",
 *   group: "system",
 *   version: "1.0.0",
 * })
 * ```
 */
export function definePlugin(def: PluginDefinition): PluginDefinition {
  return Object.freeze({ ...def })
}

import type { Plugin } from "./api"

/** Maps a PluginDefinition into the public Plugin manifest wire-shape. */
export function toPluginManifest(def: PluginDefinition): Plugin {
  return {
    id: def.id,
    name: def.name ?? def.label,
    description: def.description ?? "",
    route: def.href ?? `/plugins/${def.id}`,
    icon: def.iconName,
    group: PLUGIN_GROUP_LABELS[def.group],
    version: def.version ?? "1.0.0",
  }
}

/**
 * Normalize a plugin definition into the shape consumed by sidebar / palette.
 * Provides safe defaults (href = `/plugins/<id>`).
 */
export type LoadedPlugin = Required<Pick<PluginDefinition, "id" | "label" | "icon" | "group">> & {
  href: string
  description?: string
}

function normalize(def: PluginDefinition): LoadedPlugin {
  return {
    id: def.id,
    label: def.label,
    href: def.href ?? `/plugins/${def.id}`,
    icon: def.icon,
    description: def.description,
    group: def.group,
  }
}

/**
 * Returns all discovered plugins. Inert when no plugins are present —
 * never throws when `plugins.generated.ts` is missing or empty.
 */
export function loadPlugins(): LoadedPlugin[] {
  let raw: PluginDefinition[] = []
  try {
    // The generated module is created by `scripts/generate-plugins-manifest.mjs`.
    // It exports `plugins: PluginDefinition[]`. Using a require avoids a hard
    // build-time dependency when the file is absent (treated as empty list).
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const mod = require("./plugins.generated") as {
      plugins?: PluginDefinition[]
    }
    raw = mod?.plugins ?? []
  } catch {
    raw = []
  }
  // Defensive: skip malformed entries; de-dup by id.
  const seen = new Set<string>()
  const out: LoadedPlugin[] = []
  for (const def of raw) {
    if (!def?.id || !def?.label || !def?.icon || !def?.group) continue
    if (seen.has(def.id)) continue
    seen.add(def.id)
    out.push(normalize(def))
  }
  return out
}

/**
 * Returns the public Plugin manifest (api.ts shape) for every discovered
 * plugin. Used by the dashboard to merge with the runtime's
 * `/api/v1/plugins` response (which is intentionally empty — build-time
 * discovery is owned by the dashboard).
 *
 * Inert when no plugins are present, never throws when the generated
 * file is missing.
 */
export function loadPluginManifest(): Plugin[] {
  let raw: PluginDefinition[] = []
  try {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const mod = require("./plugins.generated") as {
      plugins?: PluginDefinition[]
    }
    raw = mod?.plugins ?? []
  } catch {
    raw = []
  }
  const seen = new Set<string>()
  const out: Plugin[] = []
  for (const def of raw) {
    if (!def?.id || !def?.label || !def?.iconName || !def?.group) continue
    if (seen.has(def.id)) continue
    seen.add(def.id)
    out.push(toPluginManifest(def))
  }
  return out
}
