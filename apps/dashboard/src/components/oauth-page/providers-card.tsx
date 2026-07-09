// SPDX-License-Identifier: Apache-2.0

"use client"

import { ExternalLink, Link2, Unlink } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { api } from "@/lib/api"
import type { OAuthConnection, OAuthProvider } from "@/lib/api"
import {
  connectionForProvider,
  extractAuthorizeUrl,
  formatExpiry,
  isProviderConnected,
  providerLabel,
} from "@/lib/oauth-page/derive"

// Providers zone — one tile per provider the runtime knows about, each
// showing whether it's configured (has client credentials) and whether a
// live tenant connection exists. Connect kicks off the authorize handshake
// and forwards the operator to the returned consent URL; Disconnect
// revokes the stored grant behind a confirm.

interface ProvidersCardProps {
  providers: OAuthProvider[]
  connections: OAuthConnection[]
  healthy: boolean
  onMutated: () => Promise<void> | void
}

export function ProvidersCard({ providers, connections, healthy, onMutated }: ProvidersCardProps) {
  const [disconnecting, setDisconnecting] = useState<OAuthProvider | null>(null)
  const connectedCount = providers.filter((p) =>
    isProviderConnected(p.provider, connections),
  ).length

  return (
    <ZoneCard aria-labelledby="oauth-providers">
      <ZoneCardHeader
        id="oauth-providers"
        title="Providers"
        subtitle={healthy ? `${connectedCount}/${providers.length} connected` : "unavailable"}
      />
      {!healthy ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          The runtime did not return OAuth providers. Start the backend with af-stack dev, confirm
          the operator key has the oauth scope, then refresh.
        </p>
      ) : providers.length === 0 ? (
        <p className="px-row-x py-tile text-meta text-muted-foreground">
          No OAuth providers registered. Declare one in the runtime config and set its client
          credentials in <code className="font-mono">Platform → Integrations</code>.
        </p>
      ) : (
        <ul
          role="list"
          className="grid grid-cols-1 gap-stack p-row-x md:grid-cols-2 xl:grid-cols-3"
        >
          {providers.map((provider) => (
            <li key={provider.provider}>
              <ProviderTile
                provider={provider}
                connection={connectionForProvider(provider.provider, connections)}
                onDisconnect={() => setDisconnecting(provider)}
              />
            </li>
          ))}
        </ul>
      )}
      <DisconnectConfirm
        provider={disconnecting}
        onClose={() => setDisconnecting(null)}
        onDisconnected={onMutated}
      />
    </ZoneCard>
  )
}

function ProviderTile({
  provider,
  connection,
  onDisconnect,
}: {
  provider: OAuthProvider
  connection: OAuthConnection | undefined
  onDisconnect: () => void
}) {
  const [connecting, setConnecting] = useState(false)
  const connected = connection !== undefined
  // A provider with no client credentials can't run a consent flow — keep
  // Connect disabled so the operator isn't sent to a dead handshake.
  const configured = provider.configured !== false
  const scopes = provider.scopes ?? []

  const connect = async () => {
    if (connecting) return
    setConnecting(true)
    try {
      const res = await api.oauth.authorize(provider.provider)
      const url = extractAuthorizeUrl(res) ?? provider.auth_url ?? null
      if (!url) {
        toast.error("No authorize URL returned", {
          description: "The runtime accepted the request but did not hand back a consent URL.",
        })
        return
      }
      // Full-page redirect into the provider's consent screen. The runtime
      // handles the callback and persists the grant; the operator lands
      // back here afterwards.
      window.location.assign(url)
    } catch (err) {
      toast.error("Could not start the authorize flow", {
        description: err instanceof Error ? err.message : String(err),
      })
      setConnecting(false)
    }
  }

  return (
    <div className="flex h-full flex-col gap-stack rounded-md border bg-card px-row-x py-row-y">
      <div className="flex items-start justify-between gap-stack">
        <div className="flex min-w-0 flex-col gap-tile-tight">
          <span className="truncate text-body font-medium text-foreground">
            {providerLabel(provider.provider)}
          </span>
          <code className="truncate font-mono text-meta text-muted-foreground">
            {provider.provider}
          </code>
        </div>
        {connected ? (
          <Badge variant="secondary">connected</Badge>
        ) : configured ? (
          <Badge variant="outline">available</Badge>
        ) : (
          <Badge variant="outline">not configured</Badge>
        )}
      </div>

      {scopes.length > 0 ? (
        <div className="flex min-w-0 flex-col gap-tile-tight">
          <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">Scopes</span>
          <span
            className="truncate font-mono text-meta text-muted-foreground"
            title={scopes.join(" ")}
          >
            {scopes.join(" ")}
          </span>
        </div>
      ) : null}

      {connected ? (
        <span className="text-meta text-muted-foreground">
          Token {formatExpiry(connection.expires_at)}.
        </span>
      ) : null}

      <div className="mt-auto flex items-center justify-end gap-inline pt-tile-tight">
        {connected ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="h-7 gap-inline text-meta text-muted-foreground hover:text-destructive"
            onClick={onDisconnect}
          >
            <Unlink className="size-3.5" aria-hidden />
            Disconnect
          </Button>
        ) : (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-7 gap-inline text-meta"
            disabled={!configured || connecting}
            onClick={connect}
            title={
              configured
                ? undefined
                : "Set this provider's client credentials in Platform → Integrations first."
            }
          >
            {connecting ? (
              <Link2 className="size-3.5 animate-pulse" aria-hidden />
            ) : (
              <ExternalLink className="size-3.5" aria-hidden />
            )}
            {connecting ? "Redirecting…" : "Connect"}
          </Button>
        )}
      </div>
    </div>
  )
}

function DisconnectConfirm({
  provider,
  onClose,
  onDisconnected,
}: {
  provider: OAuthProvider | null
  onClose: () => void
  onDisconnected: () => Promise<void> | void
}) {
  const [submitting, setSubmitting] = useState(false)

  const onConfirm = async () => {
    if (!provider || submitting) return
    setSubmitting(true)
    try {
      await api.oauth.revoke(provider.provider)
      toast.success("Provider disconnected", {
        description: providerLabel(provider.provider),
      })
      await onDisconnected()
      onClose()
    } catch (err) {
      toast.error("Could not disconnect the provider", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AlertDialog open={provider !== null} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Disconnect provider?</AlertDialogTitle>
          <AlertDialogDescription>
            The stored{" "}
            <code className="font-mono">{provider ? providerLabel(provider.provider) : ""}</code>{" "}
            grant will be revoked and its tokens deleted. Anything relying on this connection will
            stop working until it&apos;s reconnected.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel size="sm" disabled={submitting}>
            Cancel
          </AlertDialogCancel>
          <AlertDialogAction
            size="sm"
            variant="destructive"
            disabled={submitting}
            onClick={onConfirm}
          >
            {submitting ? "Disconnecting…" : "Disconnect"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
