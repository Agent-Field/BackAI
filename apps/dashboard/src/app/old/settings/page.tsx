// SPDX-License-Identifier: Apache-2.0

// Settings index. Shallow surface: operator account, appearance, feature
// flags. Account is a separate route (per docs/dashboard-ia.md) so a link card
// is offered here as well.

import Link from "next/link"
import { ArrowRight, KeyRound, ShieldCheck, UserIcon } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { requireOperator } from "@/lib/session"

import { AppearanceForm } from "./_components/appearance-form"
import { FeatureFlagsForm } from "./_components/feature-flags-form"

export default async function SettingsPage() {
  const session = await requireOperator()
  const user = session.user

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Settings"
        description="Operator account, dashboard theme, feature flags"
      />

      <Tabs defaultValue="account" className="gap-6">
        <TabsList>
          <TabsTrigger value="account">Account</TabsTrigger>
          <TabsTrigger value="appearance">Appearance</TabsTrigger>
          <TabsTrigger value="flags">Feature flags</TabsTrigger>
        </TabsList>

        <TabsContent value="account" className="flex flex-col gap-4">
          <Card>
            <CardHeader>
              <CardTitle>Operator account</CardTitle>
              <CardDescription>
                Signed in as <span className="text-foreground font-medium">{user.email}</span>.
                Update your profile, password, and sessions on the account page.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <div className="grid gap-3 sm:grid-cols-3">
                <SummaryRow icon={UserIcon} label="Name" value={user.name ?? "—"} />
                <SummaryRow icon={KeyRound} label="Email" value={user.email} />
                <SummaryRow icon={ShieldCheck} label="Role" value="Operator" />
              </div>
              <div>
                <Button
                  size="sm"
                  render={
                    <Link href="/settings/account">
                      Manage account <ArrowRight data-icon="inline-end" />
                    </Link>
                  }
                />
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="appearance">
          <Card>
            <CardHeader>
              <CardTitle>Appearance</CardTitle>
              <CardDescription>Choose how the dashboard renders for your account.</CardDescription>
            </CardHeader>
            <CardContent>
              <AppearanceForm />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="flags">
          <Card>
            <CardHeader>
              <CardTitle>Feature flags</CardTitle>
              <CardDescription>Toggle runtime feature flags for this tenant.</CardDescription>
            </CardHeader>
            <CardContent>
              <FeatureFlagsForm />
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}

function SummaryRow({
  icon: Icon,
  label,
  value,
}: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: string
}) {
  return (
    <div className="border-border/60 flex items-center gap-3 rounded-md border p-3">
      <div className="bg-muted text-foreground/80 flex aspect-square size-8 items-center justify-center rounded-md">
        <Icon className="size-4" />
      </div>
      <div className="flex min-w-0 flex-col">
        <span className="text-muted-foreground text-xs">{label}</span>
        <span className="truncate text-sm font-medium">{value}</span>
      </div>
    </div>
  )
}
