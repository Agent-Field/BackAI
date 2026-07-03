// SPDX-License-Identifier: Apache-2.0

import { CopyButton } from "@/components/ui/copy-button"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { fetchAdaptersSnapshot } from "@/lib/adapters/data"
import type { AdapterRegistrySlot } from "@/lib/api"

// Adapters / Connections (v1). Surfaces the modular connection system: every
// swappable slot, the adapter currently serving it, its health, the built-in
// alternatives, and exactly how to swap (the env var). The data comes straight
// from the runtime's adapter registry (/api/v1/admin/adapters), so what shows
// here is what the runtime actually loaded — no hard-coded list.

export const dynamic = "force-dynamic"

const STATUS_DOT: Record<string, string> = {
  healthy: "bg-emerald-500",
  degraded: "bg-amber-500",
  unhealthy: "bg-red-500",
  unknown: "bg-zinc-400",
}

const KIND_LABEL: Record<string, string> = {
  builtin: "built-in",
  remote: "remote",
  none: "unavailable",
}

function SwapHint({ slot }: { slot: AdapterRegistrySlot }) {
  if (slot.swap_method === "env_var" && slot.swap_env) {
    return (
      <div className="flex items-center gap-tile-tight">
        <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-meta text-muted-foreground">
          {slot.swap_env}
        </code>
        <CopyButton value={slot.swap_env} label="Copy env var" />
      </div>
    )
  }
  if (slot.swap_method === "connection_string" && slot.swap_env) {
    return <span className="font-mono text-meta text-muted-foreground">set {slot.swap_env}</span>
  }
  return <span className="text-meta text-muted-foreground">interface-swappable (rebuild)</span>
}

function AdapterRow({ slot }: { slot: AdapterRegistrySlot }) {
  const dot = STATUS_DOT[slot.active.status] ?? STATUS_DOT.unknown
  const alternatives = (slot.available_builtin ?? []).filter((name) => name !== slot.active.name)
  return (
    <li className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)_minmax(0,1.2fr)] items-start gap-stack px-row-x py-row-y">
      {/* Slot */}
      <div className="flex min-w-0 flex-col gap-tile-tight">
        <span className="truncate text-body font-medium text-foreground">{slot.slot}</span>
        <span className="text-eyebrow uppercase text-muted-foreground">Tier {slot.tier}</span>
      </div>

      {/* Active adapter */}
      <div className="flex min-w-0 flex-col gap-tile-tight">
        <span className="flex items-center gap-tile-tight">
          <span aria-hidden className={`size-2 shrink-0 rounded-full ${dot}`} />
          <span className="truncate font-mono text-meta text-foreground">{slot.active.name}</span>
          <span className="text-eyebrow uppercase text-muted-foreground">
            {KIND_LABEL[slot.active.kind] ?? slot.active.kind}
          </span>
        </span>
        <span className="text-meta text-muted-foreground">
          {slot.active.status}
          {slot.active.version ? ` · ${slot.active.version}` : ""}
        </span>
        {slot.active.last_error ? (
          <span className="truncate text-meta text-destructive" title={slot.active.last_error}>
            {slot.active.last_error}
          </span>
        ) : null}
      </div>

      {/* Available + swap */}
      <div className="flex min-w-0 flex-col gap-tile-tight">
        {alternatives.length > 0 ? (
          <span className="flex flex-wrap items-center gap-tile-tight">
            <span className="text-eyebrow uppercase text-muted-foreground">Available</span>
            {alternatives.map((name) => (
              <code
                key={name}
                className="rounded bg-muted px-1.5 py-0.5 font-mono text-meta text-muted-foreground"
              >
                {name}
              </code>
            ))}
          </span>
        ) : (
          <span className="text-meta text-muted-foreground">no built-in alternatives</span>
        )}
        <SwapHint slot={slot} />
      </div>
    </li>
  )
}

export default async function AdaptersPage() {
  const { slots, ok } = await fetchAdaptersSnapshot()
  const swappable = slots.filter((s) => s.swap_method !== "none").length

  return (
    <div className="flex flex-col gap-section px-page-x py-page-y">
      <header className="flex flex-col gap-tile-tight">
        <h1 className="text-xl font-semibold tracking-tight text-foreground">Adapters</h1>
        <p className="max-w-prose text-meta text-muted-foreground">
          The connection system: every backend primitive (billing, storage, sandbox, observability,
          …) is served by a swappable adapter. Swap a built-in for another, or point a slot at your
          own remote adapter — most are a one-env-var change. Write your own with{" "}
          <code className="font-mono">af-stack adapter new &lt;slot&gt;</code> (see{" "}
          <code className="font-mono">docs/adapters/AUTHORING.md</code>).
        </p>
      </header>

      <ZoneCard>
        <ZoneCardHeader
          title="Connections"
          subtitle={ok ? `${slots.length} slots · ${swappable} swappable` : "runtime unreachable"}
        />
        {slots.length === 0 ? (
          <p className="px-row-x py-tile text-meta text-muted-foreground">
            {ok
              ? "The runtime returned no adapter slots."
              : "Could not reach the runtime adapter registry. Start the backend with af-stack dev, then refresh."}
          </p>
        ) : (
          <ul role="list" className="divide-y">
            {slots.map((slot) => (
              <AdapterRow key={slot.slot} slot={slot} />
            ))}
          </ul>
        )}
      </ZoneCard>
    </div>
  )
}
