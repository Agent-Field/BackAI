// Account settings. Server component loads the session, then three Cards host
// client forms (profile, password, sessions). Each form talks to better-auth
// via the `authClient` SDK.

import Link from "next/link"
import { ArrowLeft } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { requireOperator } from "@/lib/session"

import { PasswordForm } from "./_components/password-form"
import { ProfileForm } from "./_components/profile-form"
import { SessionsPanel } from "./_components/sessions-panel"

export default async function AccountSettingsPage() {
  const session = await requireOperator()
  const user = session.user

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Account"
        description="Manage your operator profile, password, and active sessions."
        actions={
          <Button
            variant="outline"
            size="sm"
            render={
              <Link href="/settings">
                <ArrowLeft data-icon="inline-start" /> Settings
              </Link>
            }
          />
        }
      />

      <Card>
        <CardHeader>
          <CardTitle>Profile</CardTitle>
          <CardDescription>
            How you appear inside the dashboard.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ProfileForm defaultName={user.name ?? ""} email={user.email} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Password</CardTitle>
          <CardDescription>
            Change your sign-in password. Other sessions are signed out
            automatically.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <PasswordForm />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Sessions</CardTitle>
          <CardDescription>
            Devices currently signed in to this operator account.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <SessionsPanel />
        </CardContent>
      </Card>
    </div>
  )
}
