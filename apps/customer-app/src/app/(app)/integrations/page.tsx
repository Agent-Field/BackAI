// SPDX-License-Identifier: Apache-2.0

import { Plug } from "lucide-react"

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { requireCustomerContext } from "@/lib/session"
import { IntegrationsManager } from "./integrations-manager"

export const dynamic = "force-dynamic"

const RUNTIME_DEFAULT = "http://localhost:8080"

type ProviderInfo = {
  name: string
  configured: boolean
  default_scopes: string[]
}

type Connection = {
  provider: string
  scopes: string[]
  connected_at: string
  expires_at: string | null
}

// loadProviders + loadConnections call the runtime server-side so the
// page render is fully SSR (no client-side blank state). The cookie
// header is propagated via cookies() so the runtime can resolve the
// customer's tenant + user from the better-auth session.
async function loadProviders(cookieHeader: string): Promise<ProviderInfo[]> {
  const url = `${runtimeURL()}/api/v1/oauth/providers`
  try {
    const res = await fetch(url, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    })
    if (!res.ok) return []
    const body = (await res.json()) as { providers?: ProviderInfo[] }
    return body.providers ?? []
  } catch {
    return []
  }
}

async function loadConnections(cookieHeader: string): Promise<Connection[]> {
  const url = `${runtimeURL()}/api/v1/oauth/connections`
  try {
    const res = await fetch(url, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    })
    if (!res.ok) return []
    const body = (await res.json()) as { connections?: Connection[] }
    return body.connections ?? []
  } catch {
    return []
  }
}

function runtimeURL(): string {
  return process.env.RUNTIME_URL ?? RUNTIME_DEFAULT
}

async function serverCookieHeader(): Promise<string> {
  const { cookies } = await import("next/headers")
  const store = await cookies()
  return store
    .getAll()
    .map((c) => `${c.name}=${c.value}`)
    .join("; ")
}

export default async function IntegrationsPage() {
  await requireCustomerContext()
  const cookieHeader = await serverCookieHeader()
  const [providers, connections] = await Promise.all([
    loadProviders(cookieHeader),
    loadConnections(cookieHeader),
  ])
  const connectedSet = new Map(connections.map((c) => [c.provider, c]))

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight flex items-center gap-2">
          <Plug className="size-5" />
          Integrations
        </h1>
        <p className="text-muted-foreground text-sm">
          Connect third-party accounts so agents can act on your behalf.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Connected accounts</CardTitle>
          <CardDescription>
            We never store passwords. Tokens are encrypted in the BackAI vault.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <IntegrationsManager
            providers={providers}
            connections={Object.fromEntries(connectedSet)}
          />
        </CardContent>
      </Card>
    </div>
  )
}
