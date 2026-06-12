// SPDX-License-Identifier: Apache-2.0

import { Bell, Mail, ShieldCheck, UserRound, type LucideIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { requireCustomerContext } from "@/lib/session"

export const dynamic = "force-dynamic"

export default async function SettingsPage() {
  const { session, ctx } = await requireCustomerContext()
  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Account</h1>
        <p className="text-muted-foreground text-sm">
          Your profile, support preferences, and notification settings.
        </p>
      </div>

      <div className="grid gap-4 lg:grid-cols-[1fr_0.9fr]">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <UserRound className="size-4" />
              Profile
            </CardTitle>
            <CardDescription>Details support uses when helping you.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 sm:grid-cols-2">
            <InfoBlock label="Name" value={session.user.name ?? "Demo Customer"} />
            <InfoBlock label="Email" value={session.user.email ?? "demo@backai.local"} />
            <InfoBlock label="Support account" value={ctx.tenantName} />
            <div>
              <div className="text-muted-foreground text-xs">Role</div>
              <div className="mt-1">
                <Badge variant="secondary">Owner</Badge>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <ShieldCheck className="size-4" />
              Privacy and safety
            </CardTitle>
            <CardDescription>Controls a normal customer would expect.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3">
            <PreferenceRow icon={Mail} label="Email updates" value="Enabled" />
            <PreferenceRow icon={Bell} label="Request notifications" value="Enabled" />
            <PreferenceRow icon={ShieldCheck} label="Sensitive replies" value="Reviewed" />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function InfoBlock({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-muted-foreground text-xs">{label}</div>
      <div className="font-medium">{value}</div>
    </div>
  )
}

function PreferenceRow({
  icon: Icon,
  label,
  value,
}: {
  icon: LucideIcon
  label: string
  value: string
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-md border p-3">
      <div className="flex items-center gap-2">
        <Icon className="text-muted-foreground size-4" />
        <span className="text-sm">{label}</span>
      </div>
      <Badge variant="outline">{value}</Badge>
    </div>
  )
}
