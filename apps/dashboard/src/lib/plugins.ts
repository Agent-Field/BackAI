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

export type PluginGroup = "build" | "operate" | "customers" | "system"

export type PluginDefinition = {
  /** Stable id, must be unique. Matches the folder name under `plugins/`. */
  id: string
  /** Sidebar + palette label. */
  label: string
  /**
   * Route the plugin's nav item points at. Defaults to `/plugins/<id>`.
   * The generated route proxy always mounts at `/plugins/<id>` — set this
   * only if your plugin renders its own page under a different path.
   */
  href?: string
  /** Lucide icon component. */
  icon: LucideIcon
  /** Description used in the ⌘K command palette. */
  description?: string
  /** Which sidebar group the nav item joins. */
  group: PluginGroup
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
 *   description: "Example plugin",
 *   group: "system",
 * })
 * ```
 */
export function definePlugin(def: PluginDefinition): PluginDefinition {
  return Object.freeze({ ...def })
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
 * Returns all installed plugins. Inert when no plugins are present —
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
