// First-party Cost Explorer plugin (Phase 12.3).
//
// Drops the canonical example of a Plugin manifest under
// `apps/dashboard/plugins/`. Discovered at build time by the scanner
// (scripts/generate-plugins-manifest.mjs), wired into the sidebar via
// `getNavGroupsWithPlugins()`, and exposed to the runtime contract via
// `loadPluginManifest()` (api.ts Plugin shape).

import { TrendingUp } from "lucide-react"

import { definePlugin } from "@/lib/plugins"

export default definePlugin({
  id: "cost-explorer",
  label: "Cost Explorer",
  name: "Cost Explorer",
  icon: TrendingUp,
  iconName: "TrendingUp",
  description: "Top spenders and month-over-month delta.",
  group: "operate",
  version: "1.0.0",
})
