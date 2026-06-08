// Template: customer-app page (Next.js App Router).
//
// Drop into apps/customer-app/src/app/(app)/<your-route>/page.tsx.
// Pages under (app)/ require the customer to be signed in — better-auth
// middleware enforces this.
//
// You GET for free (don't reinvent):
//   - better-auth sign-up / sign-in (already wired; pages under (auth)/).
//   - On sign-up, a tenant + membership + API key are auto-provisioned
//     for the user. Their tenant_id is bound on every request to this
//     page.
//   - The customer-app layout shell (sidebar, header, theme).
//   - shadcn/ui components in @/components/ui/* and lucide-react icons.
//   - The @af-stack/sdk suite SDK for typed runtime calls.
//
// What you WRITE:
//   - Server-side data fetch via suite.* SDK helpers.
//   - The JSX. Match brand.yaml (Phase 1) / brand.css for theme.
//
// REMEMBER:
//   - Don't talk to the database directly. Always go through a workload
//     module (your code) or a suite.* SDK call.
//   - Don't reach a model provider. Always go through suite.llm.* or an
//     agent call.

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"

// Replace with your real data shape.
type Item = {
  id: string
  title: string
  created_at: string
}

export default async function REPLACE_MEPage() {
  // Server-side fetch — scoped to the customer's tenant via session +
  // RLS. The runtime resolves the tenant from the session cookie.
  const items = await fetchItems()

  return (
    <div className="container mx-auto p-6 space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">REPLACE_ME</h1>
        <p className="text-muted-foreground">
          REPLACE_ME — page subtitle for your customer.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Your items</CardTitle>
        </CardHeader>
        <CardContent>
          {items.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Nothing yet. Create your first item to get started.
            </p>
          ) : (
            <ul className="space-y-3">
              {items.map((item) => (
                <li
                  key={item.id}
                  className="flex items-center justify-between border-b last:border-0 pb-3"
                >
                  <span className="font-medium">{item.title}</span>
                  <span className="text-sm text-muted-foreground tabular-nums">
                    {new Date(item.created_at).toLocaleDateString()}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

async function fetchItems(): Promise<Item[]> {
  // The customer-app proxies /api/v1/* to the runtime with the session
  // cookie attached, so a relative URL is enough server-side.
  const res = await fetch(
    `${process.env.NEXT_PUBLIC_RUNTIME_URL ?? "http://runtime:8080"}/workload/REPLACE_ME/items`,
    {
      cache: "no-store",
      // The session cookie is on the customer-app origin; the proxy
      // layer translates it into the right tenant headers downstream.
      credentials: "include",
    },
  )
  if (!res.ok) return []
  return (await res.json()) as Item[]
}
