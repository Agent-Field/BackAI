---
title: Dashboard Plugins
description: The plugin system, schema, build-time scanner, runtime endpoint, and template for new plugin authors.
sidebar:
  order: 9
---

The dashboard ships a tiny plugin system so first-party features and
operator-authored extensions can drop into the sidebar without touching
shell code.

This document covers:

1. The Plugin schema (the public contract).
2. The build-time scanner (how plugins are discovered).
3. A template for new plugin authors.
4. The runtime endpoint contract.
5. Limitations of the v1 design.

---

## 1. Plugin schema

The wire-shape is defined once in
`apps/dashboard/src/lib/api.ts` and mirrored by the runtime. Search for
`PluginSchema` / `PluginListSchema`.

```ts
// PluginSchema (api.ts)
{
  id:          string  // stable id, matches the folder name under plugins/
  name:        string  // display name (Sidebar + ⌘K)
  description: string  // 1-line description for the command palette
  route:       string  // dashboard path the nav item points at
  icon:        string  // lucide-react icon name (resolved at render)
  group:       string  // "Build" | "Operate" | "Customers" | "Plugins"
  version:     string  // semver string, surfaced in tooling
}
```

The dashboard's *internal* `PluginDefinition`
(`apps/dashboard/src/lib/plugins.ts`)
is a superset: it carries the resolved `LucideIcon` *component* for the
sidebar plus the wire-shape fields. The scanner emits two derived
artefacts so each consumer gets the shape it needs.

| Consumer | Reads | Shape |
|---|---|---|
| Sidebar / ⌘K | `loadPlugins()` | `PluginDefinition` (with `LucideIcon` component) |
| `/api/v1/plugins` merge | `loadPluginManifest()` | `Plugin` (api.ts wire-shape) |
| Runtime endpoint | `services/runtime/internal/server/plugins.go` | empty `Plugin[]` (see §4) |

---

## 2. Build-time scanner

`apps/dashboard/scripts/generate-plugins-manifest.mjs` runs as the
`predev` and `prebuild` script (see `apps/dashboard/package.json`).

What it does on every run:

1. Walks `apps/dashboard/plugins/*/`, looking for directories that
   contain both `plugin.ts` and `page.tsx`.
2. Writes `apps/dashboard/src/lib/plugins.generated.ts` — a module that
   re-exports the imported `PluginDefinition` array.
3. Writes a Next.js route proxy at
   `apps/dashboard/src/app/(admin)/plugins/<id>/page.tsx`. Because the
   proxy lives under `(admin)`, the plugin page automatically inherits
   the dashboard chrome (sidebar, topbar, auth gate).
4. Cleans up route proxies that no longer correspond to a plugin.

The scanner uses **no TS parsing** — it discovers plugins by folder
layout and lets the TS module system handle field validation. Adding
`name`, `iconName`, or `version` fields requires no scanner edits: they
flow through the same imported manifest.

If the `plugins/` folder is empty, the scanner still runs and writes an
empty manifest. Every consumer must handle the empty case (`loadPlugins`
returns `[]`).

---

## 3. Template for new plugin authors

A new plugin is one folder under `apps/dashboard/plugins/`:

```
apps/dashboard/plugins/<id>/
├── plugin.ts   # manifest (default-exports definePlugin({...}))
└── page.tsx    # server or client component, default-export the page
```

### `plugin.ts`

```ts
import { TrendingUp } from "lucide-react"

import { definePlugin } from "@/lib/plugins"

export default definePlugin({
  id: "cost-explorer",
  label: "Cost Explorer",        // short sidebar label
  name: "Cost Explorer",         // display name (defaults to label)
  icon: TrendingUp,              // imported LucideIcon component
  iconName: "TrendingUp",        // string name (used in PluginSchema wire)
  description: "Top spenders and month-over-month delta.",
  group: "operate",              // "build" | "operate" | "customers" | "system"
  version: "1.0.0",
})
```

Field reference:

| Field | Required | Default | Notes |
|---|---|---|---|
| `id` | yes | — | Must equal the folder name. |
| `label` | yes | — | Sidebar + palette label. Keep short. |
| `name` | no | `label` | Used in the public Plugin manifest. |
| `icon` | yes | — | `LucideIcon` component for the sidebar. |
| `iconName` | yes | — | String name (e.g. `"TrendingUp"`). |
| `description` | no | `""` | Surface in the ⌘K palette. |
| `group` | yes | — | One of: `build`, `operate`, `customers`, `system`. The `system` group renders as a "Plugins" section at the bottom of the sidebar. |
| `version` | no | `"1.0.0"` | Semver. Surfaced in the Plugin manifest. |
| `href` | no | `/plugins/<id>` | Override the nav-item route. |

### `page.tsx`

Default-export a React component. Server components can use `api.*`
helpers directly; client components should hydrate from server-rendered
data. The example `apps/dashboard/plugins/cost-explorer/page.tsx` shows
the recommended pattern: fetch with `Promise.allSettled`, degrade
gracefully when the runtime is unreachable, reuse the shared
`formatCurrency` helper from `(admin)/operate/cost/_components/format.ts`.

### Run

```bash
cd apps/dashboard
pnpm dev    # predev runs the scanner; sidebar updates immediately
```

No shell edits are required. The sidebar, ⌘K palette, and route proxy
all pick up the new plugin on the next dev/build run.

---

## 4. Runtime endpoint contract

`GET /api/v1/plugins` returns the `PluginList` wire-shape. The runtime
implementation (`services/runtime/internal/server/plugins.go`) **returns
an empty list**.

Rationale: dashboard plugins are TypeScript files discovered at the
*dashboard's* build time. The runtime is a separate Go process and has
no visibility into those files. To keep the OpenAPI contract honest
without coupling the runtime to dashboard build artefacts, the runtime
serves `{plugins: []}` and the dashboard merges its own build-time
discovered plugins client-side via `loadPluginManifest()`.

If a future deployment wants a server-authoritative plugin list (so
non-dashboard clients can list them), `plugins.go` is the place to wire
that — load a JSON manifest from disk or DB and return it from
`handleListPlugins`. The TypeScript contract is already future-proof.

---

## 5. Limitations (v1)

- **Build-time only.** Plugins are bundled with the dashboard. Adding a
  plugin requires re-running `pnpm build` (or `pnpm dev` for hot reload).
  There is **no client-side dynamic loading** — plugin code is part of
  the dashboard Next.js bundle.
- **No sandboxing.** Plugins run with the full power of the dashboard.
  Only ship plugins you trust.
- **TypeScript only.** v1 expects `plugin.ts` + `page.tsx` with a
  default-exported React component. JSON manifests + remote bundles are
  future work.
- **Single source of truth is the dashboard.** The runtime endpoint is
  intentionally empty. If you need server-authoritative plugins, see §4.
- **No plugin lifecycle hooks.** v1 has no on-install / on-uninstall
  hooks, no per-tenant enablement, no version-resolution. Plugins just
  exist as long as their folder exists in the dashboard tree.
