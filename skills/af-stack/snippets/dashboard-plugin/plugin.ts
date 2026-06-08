// Template: AF Stack dashboard plugin manifest.
//
// Drop into apps/dashboard/plugins/<your-id>/plugin.ts.
// Edit id, label, name, description, icon, group, version.
//
// The plugin scanner (apps/dashboard/scripts/generate-plugins-manifest.mjs)
// discovers this file at build time, adds it to the sidebar, and wires
// the route at /plugins/<id> to render page.tsx.
//
// Group choices (current state):
//   - "operate"   — what's running right now (default for monitoring/charts)
//   - "build"     — your product config (agents, modules, integrations)
//   - "customers" — customer-management views (per-tenant breakdowns)
//
// Pick ICON from lucide-react: https://lucide.dev/icons/.
//
// REMEMBER: dashboard plugins are READ-ONLY (per rules/dashboard-plugins.md).
// They observe state from the runtime; they don't write config. Config
// happens in .env / brand.yaml / runtime APIs the user owns.

import { TrendingUp } from "lucide-react"

import { definePlugin } from "@/lib/plugins"

export default definePlugin({
  // Unique slug. Lowercase + kebab-case. Matches folder name.
  id: "REPLACE_ME",

  // Short label shown in the sidebar (~12 chars).
  label: "REPLACE_ME",

  // Full name shown on the page header.
  name: "REPLACE_ME",

  // The lucide-react component AND its string name (the scanner needs
  // both because it generates a manifest from this file at build time).
  icon: TrendingUp,
  iconName: "TrendingUp",

  // One-line description for the plugin index.
  description: "REPLACE_ME — one-line description of what this plugin shows.",

  // Which sidebar group. See above for choices.
  group: "operate",

  // Semver — bump on every change to this manifest.
  version: "0.1.0",
})
