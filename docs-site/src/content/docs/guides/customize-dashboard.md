---
title: Customize the Dashboard
description: Reskin, add tabs, swap the logo — without forking.
sidebar:
  order: 1
---

The dashboard ships with three customisation surfaces, in increasing
order of commitment: theming (5 minutes), plugins (30 minutes), and a
hard fork (don't, if you can avoid it).

## Theming (5 minutes)

Every colour, radius, and chart variable is a CSS custom property.
Override the variables in `apps/dashboard/src/app/brand.css`, import it
once after `globals.css`, and your palette propagates through every
shadcn primitive, every chart, and every plugin tab.

```css
/* apps/dashboard/src/app/brand.css */
:root {
  --primary: oklch(0.55 0.21 145); /* emerald */
  --accent: oklch(0.96 0.04 145);
  --chart-1: oklch(0.65 0.21 145);
}

.dark {
  --primary: oklch(0.78 0.18 145);
  --accent: oklch(0.32 0.08 145);
}
```

```tsx
// apps/dashboard/src/app/layout.tsx
import "./globals.css"
import "./brand.css" // <-- add this line
```

Full token surface is in [Theming](/guides/theming/).

## Logo and favicon

Sidebar wordmark is at
`apps/dashboard/src/app/(admin)/_layout/sidebar.tsx`. Replace the
markup, or expose your logo as a data URL in a CSS variable and
reference it from the existing markup.

Favicons live under `apps/dashboard/public/`. Drop in `favicon.ico`,
`icon.png`, `apple-icon.png` and Next.js picks them up on next build.

## Plugin tabs (30 minutes)

Each plugin is a directory under `apps/dashboard/plugins/<id>/` with
a manifest and a page component:

```
apps/dashboard/plugins/my-plugin/
  plugin.ts        # manifest matching PluginSchema
  page.tsx         # the actual page
```

```ts
// apps/dashboard/plugins/my-plugin/plugin.ts
import type { Plugin } from "@/lib/api"

const plugin: Plugin = {
  id: "my-plugin",
  name: "My Plugin",
  description: "What this tab does.",
  route: "/plugins/my-plugin",
  icon: "Sparkles",      // any lucide icon name
  group: "Operate",      // Build | Operate | Customers | Plugins
  version: "0.1.0",
}
export default plugin
```

```tsx
// apps/dashboard/plugins/my-plugin/page.tsx
import { api } from "@/lib/api"

export default async function MyPluginPage() {
  const cost = await api.cost()
  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl">My plugin</h1>
      <p>Period total: ${cost.period_total_usd.toFixed(4)}</p>
    </div>
  )
}
```

On the next `npm run dev` (or `pnpm build`), the plugin scanner
discovers the manifest, generates
`apps/dashboard/src/lib/plugins.generated.ts`, and the sidebar nav
adds your tab under its declared group.

Working reference: [`examples/01-notable/dashboard-plugin/`](https://github.com/Agent-Field/backai/tree/main/examples/01-notable/dashboard-plugin)
— a real `plugin.ts` + `page.tsx` pair you can copy into
`apps/dashboard/plugins/<id>/`. No plugins ship enabled by default; the
directory is created by `af-stack plugin new <id>`.
Full guide: [Reference → Dashboard Plugins](/reference/dashboard-plugins/).

## What you DON'T need to fork for

- Adding a route (use a plugin).
- Changing colours (use brand.css).
- Adding a chart or KPI (use a plugin + the existing `api` client).
- Changing the auth flow (better-auth is already swappable via env).
- Adding a new sidebar group ("Plugins" is auto-added when any plugin
  declares `group: "Plugins"`).

## What you SHOULD fork for

- Replacing the sidebar layout entirely.
- Removing built-in tabs (delete them from `nav.ts`).
- Hooking into the auth flow at a level the existing pluggable
  primitives don't expose.

## Verify it worked

After theming changes:

```bash
docker compose build dashboard && docker compose up -d dashboard
open http://localhost:33000
```

Toggle dark mode, click through the sidebar, open a Dialog. Your
palette should read cleanly in both modes. The Cost tab is the
chart-heaviest view — that's where chart palette mistakes show first.

After adding a plugin:

```bash
cd apps/dashboard && npm run build
# Look for: [plugins] Discovered: my-plugin
```

Then visit `/plugins/my-plugin` in the running dashboard.
