// Example plugin.
//
// Adding a plugin = drop a folder under `apps/dashboard/plugins/<id>/` with:
//   - plugin.ts  (this file): default-exports `definePlugin({...})`.
//   - page.tsx              : default-exports a React component (server or client).
//
// The next `pnpm dev` / `pnpm build` runs `scripts/generate-plugins-manifest.mjs`
// which auto-discovers the folder, regenerates `src/lib/plugins.generated.ts`,
// and writes a route proxy at `src/app/(admin)/plugins/<id>/page.tsx`.
//
// The nav item appears in the sidebar and ⌘K palette under the chosen group
// (`build`, `operate`, `customers`, or `system`) — no shell edits needed.

import { Sparkles } from "lucide-react"

import { definePlugin } from "@/lib/plugins"

export default definePlugin({
  id: "hello",
  label: "Hello",
  icon: Sparkles,
  iconName: "Sparkles",
  description: "Example plugin — proves the loader works end-to-end",
  group: "system",
  version: "1.0.0",
})
