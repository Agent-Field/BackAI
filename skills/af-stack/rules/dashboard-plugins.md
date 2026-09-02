# Dashboard Plugins — operator-console tabs

A dashboard plugin adds a tab to the operator console (`apps/dashboard`).
Pure read-only UI; the config it shows lives in env vars / brand.yaml.

## The shape

```
apps/dashboard/plugins/<your-id>/
├── plugin.ts         # manifest (id, label, icon, group, version)
└── page.tsx          # React Server Component that renders the page
```

Both files are required. The scanner
(`apps/dashboard/scripts/generate-plugins-manifest.mjs`) discovers them
at build time, registers the sidebar entry, and generates the proxy
route at `apps/dashboard/src/app/(admin)/plugins/<id>/page.tsx`.

Copy from `snippets/dashboard-plugin/` to start.

## The manifest

```ts
import { TrendingUp } from "lucide-react"
import { definePlugin } from "@/lib/plugins"

export default definePlugin({
  id: "your-id",                // unique slug
  label: "Your Plugin",          // sidebar text (~12 chars)
  name: "Your Plugin",           // page header
  icon: TrendingUp,              // lucide-react component
  iconName: "TrendingUp",        // string version (scanner needs both)
  description: "Per-tenant breakdown of ...",
  group: "operate",              // "build" | "operate" | "customers"
  version: "0.1.0",
})
```

## Group choice

| Group | When | Examples |
|---|---|---|
| `build` | Your product config: agents, modules, integrations | Future: a "module config" view |
| `operate` | Live runtime state: charts, status, lists | Cost/usage charts, run status, queue depth |
| `customers` | Per-tenant / per-end-user views | `notable` example plugin (per-tenant note counts) |

If unsure: `operate` is the default for monitoring views.

## The page

React Server Component (`async function Page()`). The dashboard layout
shell wraps it; you just render content.

```tsx
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card"

export default async function YourPluginPage() {
  const data = await fetchData()  // server-side fetch

  return (
    <div className="container mx-auto p-6 space-y-6">
      <h1 className="text-3xl font-semibold">Your Plugin</h1>
      <Card>
        <CardHeader><CardTitle>Stats</CardTitle></CardHeader>
        <CardContent>{/* your content */}</CardContent>
      </Card>
    </div>
  )
}
```

### What you GET for free

- **Layout shell**: sidebar, header, theme. Don't reinvent.
- **Auth + tenant context**: the operator's session is bound; your
  server-side fetches inherit it.
- **shadcn/ui components**: `@/components/ui/*` — Card, Table, Badge,
  Button, Tabs, Sheet, Dialog, etc.
- **Icons**: `lucide-react` is in deps.
- **Charts**: `recharts` is in deps.
- **`@/lib/api`**: typed runtime fetchers (when adding standard routes).

### What you WRITE

- **Server-side data fetch**: `fetch()` against the runtime or your
  workload module's stats endpoint.
- **JSX**: keep it shadcn-clean (per the user's project's `af-ui` skill).
- **Optional client components**: if you need interactivity (sorts,
  filters), use `"use client"` in a sub-component, not the page itself.

## Where to fetch data

| Source | Endpoint shape | Example |
|---|---|---|
| Core runtime routes | `${process.env.RUNTIME_URL}/api/v1/...` | `/api/v1/cost/by-tenant` |
| Your workload module | `${process.env.RUNTIME_URL}/workload/<id>/...` | `/workload/forge/stats` |
| AgentField (via runtime) | `${process.env.RUNTIME_URL}/api/v1/agents/...` | `/api/v1/agents/<name>` |

Use `cache: "no-store"` on `fetch()` for live data. The operator console
is server-rendered each request so the operator sees fresh state.

## Charts

`recharts` is already a dependency (`apps/dashboard/package.json`).
Charts must be client components:

```tsx
"use client"  // recharts needs the client

import { LineChart, Line, XAxis, YAxis, Tooltip } from "recharts"

export function CostChart({ data }: { data: Array<{ day: string; usd: number }> }) {
  return (
    <LineChart width={600} height={300} data={data}>
      <XAxis dataKey="day" />
      <YAxis />
      <Tooltip />
      <Line type="monotone" dataKey="usd" stroke="hsl(var(--primary))" />
    </LineChart>
  )
}
```

## Anti-patterns

| Anti-pattern | Why wrong | Correct |
|---|---|---|
| Settings panel that writes env vars | Config lives in `.env` / `brand.yaml` | Read-only display; link to docs |
| Form to create a tenant from the plugin | Tenants are operator-managed via existing pages | Use `/customers/tenants` |
| Direct DB queries from a server component | Bypass the runtime | `fetch()` the runtime / workload module |
| Custom auth on the plugin route | The plugin runs inside the operator-authed area | Inherit the session |
| Hardcoded brand colors | Brand state belongs in `brand.yaml` | Use Tailwind tokens (`text-primary`, `bg-card`) |
| Manual `dark:` color overrides | The dashboard's color tokens auto-swap in dark mode | Use semantic tokens |
| Reinventing Card / Table / Badge | shadcn primitives are right there | Use `@/components/ui/*` |
| Hosting heavy client-side computation in the plugin | Plugin pages should be fast | Move computation to the workload module |

## Where the operator opens your plugin

Sidebar → your `group` → your `label`. Route: `/plugins/<id>`.

Operator should be able to bookmark `https://admin.your-domain.com/plugins/<id>`
and arrive directly.

## Versioning

Bump `version` in the manifest on every meaningful change. Future
extensibility — once the plugin index ships, operators will see
"version bumped" badges. Today the version is just metadata.

## Testing locally

```bash
# In the customer's fork
pnpm --filter dashboard build
# or for dev
pnpm --filter dashboard dev
```

The scanner re-runs at build time. If your plugin doesn't appear in the
sidebar, check `apps/dashboard/src/lib/plugins.generated.ts` — it
should list your plugin's id.
