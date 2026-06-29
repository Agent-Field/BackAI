# BackAI Admin Dashboard

Operator console for a BackAI deployment. This app is for the team
running the AI product: tenants, API keys, costs, runs, jobs, storage,
billing, plugins, and platform operations.

For fork customization rules, see [`EDITING.md`](EDITING.md). For the
overall repo ownership model, see [`docs/repo-map.md`](../../docs/repo-map.md).

## Local Development

```bash
pnpm dev
```

From the root stack, the dashboard runs at `http://localhost:33000`.
When developing only this app, make sure the runtime API URL points at a
running BackAI runtime.

## What To Customize

Prefer adding domain-specific admin views as plugins under
`apps/dashboard/plugins/<id>/`. Change the shared dashboard shell only
when the capability is useful to every BackAI fork.

## Add A Plugin

A plugin is a contributor-owned folder under `apps/dashboard/plugins/<id>/`.
The shell auto-discovers it — no edits to `nav.ts`, the sidebar, or the
command palette.

A plugin is two files:

```
apps/dashboard/plugins/<id>/
├── plugin.ts   # manifest — default-exports definePlugin({...})
└── page.tsx    # page — default-exports a React component (server or client)
```

Example (`apps/dashboard/plugins/hello/plugin.ts`):

```ts
import { Sparkles } from "lucide-react"
import { definePlugin } from "@/lib/plugins"

export default definePlugin({
  id: "hello",
  label: "Hello",
  icon: Sparkles,
  description: "Example plugin",
  group: "system", // build | operate | customers | system
})
```

The next `pnpm dev` or `pnpm build` runs
`scripts/generate-plugins-manifest.mjs`, which:

1. Regenerates `src/lib/plugins.generated.ts` (the manifest `loadPlugins()` reads).
2. Writes a route proxy at `src/app/(admin)/plugins/<id>/page.tsx` so the page
   inherits the dashboard chrome (sidebar, topbar, auth gate).

Both generated paths are gitignored. The plugin appears in the sidebar group
chosen by `group`, in ⌘K, and on the Settings → Plugins tab.

To regenerate manually:

```bash
pnpm generate:plugins
```
