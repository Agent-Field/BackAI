// Auth tab — identity providers configured for the dashboard/customer app.

import { CircleCheck, Mail, ShieldCheck, X } from "lucide-react"

import { PageHeader } from "@/components/layout/page-header"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { getDashboardSSOConfig } from "@/lib/sso"

export const dynamic = "force-dynamic"

type Provider = {
  id: string
  name: string
  envVars: string[]
  enabled: boolean
  description: string
}

function detectProviders(): Provider[] {
  const sso = getDashboardSSOConfig()
  return [
    {
      id: "email",
      name: "Email + password",
      envVars: [],
      enabled: true,
      description:
        "Default and always on. Better-auth handles password hashing with bcrypt cost 12.",
    },
    {
      id: "google",
      name: "Google OAuth",
      envVars: ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"],
      enabled:
        !!process.env.GOOGLE_CLIENT_ID &&
        !!process.env.GOOGLE_CLIENT_SECRET,
      description:
        "Sign in with Google. Add the env vars and restart the dashboard to enable.",
    },
    {
      id: "enterprise-sso",
      name: sso.label,
      envVars: [
        "AF_STACK_SSO_ISSUER",
        "AF_STACK_SSO_CLIENT_ID",
        "AF_STACK_SSO_CLIENT_SECRET",
      ],
      enabled: sso.enabled,
      description:
        "Enterprise SSO through an OIDC provider. Use Authentik to bridge SAML IdPs in self-hosted installs, or WorkOS as the managed SSO broker.",
    },
  ]
}

export default async function Page() {
  const providers = detectProviders()
  const enabledCount = providers.filter((p) => p.enabled).length

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Auth"
        description="Identity providers used by the dashboard and customer-facing app. SSO/SAML is handled through an OIDC bridge so sessions stay in better-auth/Postgres."
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <CircleCheck className="size-3" />
              Enabled
            </CardDescription>
            <CardTitle className="text-4xl font-semibold tabular-nums tracking-tight">
              {enabledCount}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <Mail className="size-3" />
              Default
            </CardDescription>
            <CardTitle className="text-2xl font-semibold tracking-tight">
              Email + password
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription className="flex items-center gap-1.5">
              <ShieldCheck className="size-3" />
              Backend
            </CardDescription>
            <CardTitle className="text-2xl font-semibold tracking-tight">
              better-auth
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {providers.map((p) => (
          <Card key={p.id}>
            <CardHeader>
              <div className="flex items-center justify-between gap-2">
                <CardTitle className="text-lg">{p.name}</CardTitle>
                <Badge variant={p.enabled ? "default" : "outline"}>
                  {p.enabled ? (
                    <>
                      <CircleCheck className="size-3" /> Enabled
                    </>
                  ) : (
                    <>
                      <X className="size-3" /> Not configured
                    </>
                  )}
                </Badge>
              </div>
              <CardDescription>{p.description}</CardDescription>
            </CardHeader>
            <CardContent>
              {p.envVars.length === 0 ? (
                <p className="text-muted-foreground text-xs">
                  No environment configuration required.
                </p>
              ) : (
                <div className="flex flex-col gap-1.5">
                  <span className="text-muted-foreground text-xs">
                    Required env vars
                  </span>
                  <div className="flex flex-wrap gap-1.5">
                    {p.envVars.map((v) => (
                      <code
                        key={v}
                        className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs"
                      >
                        {v}
                      </code>
                    ))}
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Session storage</CardTitle>
          <CardDescription>
            Better-auth persists sessions in Postgres (table:{" "}
            <code className="font-mono">session</code>). All replicas
            read the same session row — no sticky-session requirement.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 gap-2 text-sm md:grid-cols-2">
            <div className="flex justify-between gap-4">
              <span className="text-muted-foreground">Cookie name</span>
              <code className="font-mono text-xs">
                better-auth.session_token
              </code>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-muted-foreground">Session table</span>
              <code className="font-mono text-xs">session</code>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-muted-foreground">User table</span>
              <code className="font-mono text-xs">user</code>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-muted-foreground">Suite mirror</span>
              <code className="font-mono text-xs">suite_users</code>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Enterprise SSO callback</CardTitle>
          <CardDescription>
            Register this operator-dashboard redirect URI with Authentik,
            WorkOS, Okta, Entra ID, Keycloak, or another OIDC broker. The
            customer app uses the same path on its own host.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <code className="bg-muted rounded px-2 py-1 font-mono text-xs">
            {(process.env.BETTER_AUTH_URL || "https://admin.example.com") +
              "/api/auth/oauth2/callback/enterprise-sso"}
          </code>
        </CardContent>
      </Card>
    </div>
  )
}
