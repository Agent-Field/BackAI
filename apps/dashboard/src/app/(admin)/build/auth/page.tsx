// SPDX-License-Identifier: Apache-2.0

// Auth page — Build → Auth. Identity providers the runtime can broker
// OAuth-on-behalf-of-user tokens for, backed by GET /api/v1/oauth/providers.
// Server component: fetched once per render (force-dynamic + no-store via
// lib/api.ts). When the runtime isn't reachable we render a clean empty-state
// shell rather than crashing.

import { KeyRound, ShieldCheck } from "lucide-react"

import { api, ApiError, type OAuthProviderList } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { PageHeader } from "@/components/layout/page-header"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export const dynamic = "force-dynamic"

async function loadProviders(): Promise<{
  data: OAuthProviderList
  error: string | null
}> {
  try {
    const data = await api.oauth.providers()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load OAuth providers"
    return { data: { providers: [] }, error: message }
  }
}

export default async function Page() {
  const { data, error } = await loadProviders()
  const providers = data.providers

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Auth"
        description="Identity providers the runtime can broker OAuth-on-behalf-of-user tokens for. Backed by /api/v1/oauth/providers."
      />

      {error && providers.length === 0 ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
          <p className="text-foreground mb-1 font-medium">
            OAuth providers aren&apos;t available
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            .
          </p>
        </div>
      ) : providers.length === 0 ? (
        <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
          <KeyRound className="size-3.5" />
          No OAuth providers compiled into this runtime.
        </div>
      ) : (
        <div className="bg-card rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Provider
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Configured
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Default scopes
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {providers.map((provider) => (
                <TableRow key={provider.name} className="hover:bg-muted/30">
                  <TableCell>
                    <code className="font-mono text-xs">{provider.name}</code>
                  </TableCell>
                  <TableCell>
                    {provider.configured ? (
                      <Badge variant="secondary" className="text-xs">
                        Configured
                      </Badge>
                    ) : (
                      <Badge variant="outline" className="text-xs">
                        Not configured
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    {provider.default_scopes.length > 0 ? (
                      <div className="flex flex-wrap items-center gap-1">
                        <ShieldCheck className="text-muted-foreground size-3.5" />
                        {provider.default_scopes.map((scope) => (
                          <Badge
                            key={scope}
                            variant="outline"
                            className="text-xs"
                          >
                            {scope}
                          </Badge>
                        ))}
                      </div>
                    ) : (
                      <span className="text-muted-foreground text-xs">—</span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <p className="text-muted-foreground text-xs">
        Set <code className="font-mono">OAUTH_&lt;NAME&gt;_CLIENT_ID</code> and{" "}
        <code className="font-mono">OAUTH_&lt;NAME&gt;_CLIENT_SECRET</code> to
        configure a provider.
      </p>
    </div>
  )
}
