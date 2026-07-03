// SPDX-License-Identifier: Apache-2.0

import { AppSidebar } from "@/components/app-sidebar"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbList,
  BreadcrumbPage,
} from "@/components/ui/breadcrumb"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Separator } from "@/components/ui/separator"
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar"
import { brand } from "@/lib/brand"
import { requireCustomerContext } from "@/lib/session"

// Real customer dashboard: shows the signed-in customer their account +
// tenant + a getting-started panel for calling the backend. Replaced the
// shadcn starter's placeholder gray boxes.

export const dynamic = "force-dynamic"

export default async function DashboardPage() {
  const { session, ctx } = await requireCustomerContext()
  const user = { name: session.user.name, email: session.user.email }

  return (
    <SidebarProvider>
      <AppSidebar user={user} />
      <SidebarInset>
        <header className="flex h-16 shrink-0 items-center gap-2">
          <div className="flex items-center gap-2 px-4">
            <SidebarTrigger className="-ml-1" />
            <Separator
              orientation="vertical"
              className="mr-2 data-vertical:h-4 data-vertical:self-auto"
            />
            <Breadcrumb>
              <BreadcrumbList>
                <BreadcrumbItem>
                  <BreadcrumbPage>Dashboard</BreadcrumbPage>
                </BreadcrumbItem>
              </BreadcrumbList>
            </Breadcrumb>
          </div>
        </header>

        <div className="flex flex-1 flex-col gap-6 p-4 pt-0">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">
              Welcome{user.name ? `, ${user.name}` : ""}
            </h1>
            <p className="text-sm text-muted-foreground">
              Your {brand.displayName} account and backend are ready.
            </p>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Account</CardTitle>
                <CardDescription>Your customer details.</CardDescription>
              </CardHeader>
              <CardContent className="grid gap-3 text-sm">
                <Detail label="Email" value={user.email} mono />
                <Detail label="Workspace" value={ctx.tenantName} />
                <Detail label="Workspace ID" value={ctx.tenantId} mono />
                <Detail
                  label="API key"
                  value={ctx.apiKeyPrefix ? `${ctx.apiKeyPrefix}…` : "no key yet — see Get started"}
                  mono={!!ctx.apiKeyPrefix}
                />
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Get started</CardTitle>
                <CardDescription>Call your backend over one base URL.</CardDescription>
              </CardHeader>
              <CardContent className="grid gap-3 text-sm">
                <p className="text-muted-foreground">
                  Your backend exposes auth, storage, billing, agents, and an OpenAI-compatible LLM
                  gateway behind a single API. Use your API key as a bearer token:
                </p>
                <pre className="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs">
                  {`curl $AF_STACK_URL/api/v1/agents \\
  -H "Authorization: Bearer $AF_STACK_API_KEY"`}
                </pre>
                <p className="text-muted-foreground">
                  Prefer a typed client? Scaffold an app with{" "}
                  <code className="font-mono">af-stack init my-app</code>.
                </p>
              </CardContent>
            </Card>
          </div>
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}

function Detail({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-4">
      <span className="text-muted-foreground">{label}</span>
      <span
        className={`min-w-0 truncate text-right ${mono ? "font-mono text-xs" : "font-medium"}`}
        title={value}
      >
        {value}
      </span>
    </div>
  )
}
