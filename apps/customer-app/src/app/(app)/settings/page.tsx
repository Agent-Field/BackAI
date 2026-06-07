// SPDX-License-Identifier: Apache-2.0

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { requireCustomerContext } from "@/lib/session"

export const dynamic = "force-dynamic"

export default async function SettingsPage() {
  const { session, ctx } = await requireCustomerContext()
  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="text-muted-foreground text-sm">
          Account and tenant info.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Account</CardTitle>
          <CardDescription>From your better-auth session.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2">
          <div>
            <div className="text-xs text-muted-foreground">Name</div>
            <div className="font-medium">{session.user.name ?? "—"}</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground">Email</div>
            <div className="font-medium">{session.user.email}</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground">User ID</div>
            <div className="font-mono text-xs">{session.user.id}</div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Tenant</CardTitle>
          <CardDescription>
            Your customer-app sees one tenant per signup.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2">
          <div>
            <div className="text-xs text-muted-foreground">Tenant name</div>
            <div className="font-medium">{ctx.tenantName}</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground">Slug</div>
            <div className="font-mono text-xs">{ctx.tenantSlug}</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground">Tenant ID</div>
            <div className="font-mono text-xs">{ctx.tenantId}</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground">Role</div>
            <div>
              <Badge>owner</Badge>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
