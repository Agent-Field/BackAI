// SPDX-License-Identifier: Apache-2.0

"use client"

import { useEffect, useState, useTransition } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import { Check, Link2, Loader2, Plug, Unplug } from "lucide-react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

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

type Props = {
  providers: ProviderInfo[]
  connections: Record<string, Connection>
}

function prettyName(name: string): string {
  switch (name) {
    case "google":
      return "Google"
    case "github":
      return "GitHub"
    case "notion":
      return "Notion"
    case "slack":
      return "Slack"
    case "linear":
      return "Linear"
    default:
      return name
  }
}

export function IntegrationsManager({ providers, connections }: Props) {
  const router = useRouter()
  const searchParams = useSearchParams()
  const [pending, startTransition] = useTransition()
  const [busy, setBusy] = useState<string | null>(null)

  // React to the callback query params (?oauth=google&status=connected)
  // so the user sees a toast after the OAuth dance redirects back.
  useEffect(() => {
    const oauth = searchParams.get("oauth")
    const status = searchParams.get("status")
    const oauthErr = searchParams.get("oauth_error")
    if (oauthErr) {
      toast.error(`OAuth failed: ${oauthErr}`)
      // Strip query params so refreshes don't re-toast.
      router.replace("/integrations")
      return
    }
    if (oauth && status === "connected") {
      toast.success(`Connected ${prettyName(oauth)}.`)
      router.replace("/integrations")
    }
  }, [searchParams, router])

  const handleConnect = async (name: string) => {
    setBusy(name)
    try {
      const res = await fetch(`/api/v1/oauth/${encodeURIComponent(name)}/authorize`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          return_to: `${window.location.origin}/integrations`,
        }),
      })
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as {
          error?: { message?: string }
        } | null
        toast.error(
          body?.error?.message ?? `Could not start OAuth: HTTP ${res.status}`,
        )
        return
      }
      const data = (await res.json()) as { authorization_url: string }
      window.location.href = data.authorization_url
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  const handleDisconnect = (name: string) => {
    setBusy(name)
    startTransition(async () => {
      try {
        const res = await fetch(`/api/v1/oauth/${encodeURIComponent(name)}`, {
          method: "DELETE",
          credentials: "include",
        })
        if (!res.ok) {
          toast.error(`Could not disconnect: HTTP ${res.status}`)
          return
        }
        toast.success(`Disconnected ${prettyName(name)}.`)
        router.refresh()
      } catch (err) {
        toast.error(err instanceof Error ? err.message : String(err))
      } finally {
        setBusy(null)
      }
    })
  }

  if (providers.length === 0) {
    return (
      <p className="text-muted-foreground py-6 text-center text-sm">
        No OAuth providers configured on this runtime.
      </p>
    )
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Provider</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Scopes</TableHead>
          <TableHead>Connected</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {providers.map((p) => {
          const conn = connections[p.name] ?? null
          const isConnected = conn !== null
          const isBusy = busy === p.name || pending
          return (
            <TableRow key={p.name}>
              <TableCell className="font-medium flex items-center gap-2">
                <Plug className="size-3.5 text-muted-foreground" />
                {prettyName(p.name)}
              </TableCell>
              <TableCell>
                {!p.configured ? (
                  <Badge variant="outline">Not configured</Badge>
                ) : isConnected ? (
                  <Badge>
                    <Check className="size-3 mr-1" />
                    Connected
                  </Badge>
                ) : (
                  <Badge variant="outline">Not connected</Badge>
                )}
              </TableCell>
              <TableCell className="text-xs">
                {(conn?.scopes ?? p.default_scopes).slice(0, 4).map((s) => (
                  <Badge
                    key={s}
                    variant="outline"
                    className="mr-1 mb-1 text-[10px]"
                  >
                    {s}
                  </Badge>
                ))}
                {(conn?.scopes ?? p.default_scopes).length > 4 ? (
                  <span className="text-muted-foreground text-[10px]">
                    +{(conn?.scopes ?? p.default_scopes).length - 4} more
                  </span>
                ) : null}
              </TableCell>
              <TableCell className="text-muted-foreground text-xs">
                {conn ? new Date(conn.connected_at).toLocaleString() : "—"}
              </TableCell>
              <TableCell className="text-right">
                {!p.configured ? (
                  <span className="text-muted-foreground text-xs">
                    Set env keys
                  </span>
                ) : isConnected ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleDisconnect(p.name)}
                    disabled={isBusy}
                  >
                    {isBusy ? (
                      <Loader2 className="size-3.5 animate-spin" />
                    ) : (
                      <Unplug className="size-3.5" />
                    )}
                    Disconnect
                  </Button>
                ) : (
                  <Button
                    size="sm"
                    onClick={() => handleConnect(p.name)}
                    disabled={isBusy}
                  >
                    {isBusy ? (
                      <Loader2 className="size-3.5 animate-spin" />
                    ) : (
                      <Link2 className="size-3.5" />
                    )}
                    Connect
                  </Button>
                )}
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}
