// Template: AF Stack dashboard plugin page (React Server Component).
//
// Drop into apps/dashboard/plugins/<your-id>/page.tsx.
//
// This is rendered at /plugins/<your-id> in the operator console.
// Use it for read-only views: lists, charts, status, breakdowns.
//
// You GET for free (don't reinvent):
//   - Auth + tenant context (the operator's session is already bound;
//     server-side fetches inherit it).
//   - The dashboard layout shell (sidebar, header, theme).
//   - shadcn/ui components in @/components/ui/* and lucide-react icons.
//   - The api helper in @/lib/api for typed runtime fetches.
//   - Charts via recharts (already a dependency).
//
// What you WRITE:
//   - Server-side data fetch via api.fetch(<openapi-route>) — see
//     /api/v1/openapi.json for available routes.
//   - The JSX. Keep it shadcn-clean; don't reinvent design language.
//
// REMEMBER: read-only. No forms that write to env. No "settings" panels.

import { TrendingUp } from "lucide-react"

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"

// Typed interface for what this page renders. Define a Zod schema in
// @/lib/api if you want runtime validation of the fetch response.
type Stats = {
  total_today: number
  cost_today_usd: number
  top_items: { name: string; count: number }[]
}

export default async function REPLACE_MEPage() {
  // Server-side fetch. The dashboard's auth + tenant context is
  // automatically propagated (the runtime sees the operator's session).
  // Replace the URL with your workload module's route, e.g.
  // /workload/<your-module-id>/stats or a runtime route.
  const stats = await fetchStats()

  return (
    <div className="container mx-auto p-6 space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">REPLACE_ME</h1>
        <p className="text-muted-foreground">
          REPLACE_ME — page subtitle.
        </p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Total today
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-semibold">{stats.total_today}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Cost today
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-semibold">
              ${stats.cost_today_usd.toFixed(2)}
            </div>
          </CardContent>
        </Card>

        <Card className="md:col-span-1">
          <CardHeader>
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Status
            </CardTitle>
          </CardHeader>
          <CardContent className="flex items-center gap-2">
            <TrendingUp className="h-4 w-4 text-emerald-500" />
            <span className="text-sm">Healthy</span>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Top items</CardTitle>
          <CardDescription>Most-active items in the last 24h.</CardDescription>
        </CardHeader>
        <CardContent>
          <ul className="space-y-2">
            {stats.top_items.map((item) => (
              <li
                key={item.name}
                className="flex items-center justify-between border-b last:border-0 pb-2"
              >
                <span className="text-sm">{item.name}</span>
                <span className="text-sm tabular-nums text-muted-foreground">
                  {item.count}
                </span>
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  )
}

// REPLACE: point at your real backend route.
async function fetchStats(): Promise<Stats> {
  const res = await fetch(
    `${process.env.RUNTIME_URL ?? "http://runtime:8080"}/workload/REPLACE_ME/stats`,
    { cache: "no-store" },
  )
  if (!res.ok) {
    return { total_today: 0, cost_today_usd: 0, top_items: [] }
  }
  return (await res.json()) as Stats
}
