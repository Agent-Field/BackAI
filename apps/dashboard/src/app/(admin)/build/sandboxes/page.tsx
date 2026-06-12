// Sandbox adapter configuration page.
//
// Server-renders the active adapter, its capabilities, the path to switch
// adapters via config.yaml, the network mode reference table, and the
// resource defaults. Activity (pool counts, recent runs, logs) lives on
// the Operate > Sandbox Activity tab.

import { PageHeader } from "@/components/layout/page-header"
import { api, type SandboxPoolStats } from "@/lib/api"

import { SandboxesConfigView } from "./_components/sandboxes-config-view"

export const dynamic = "force-dynamic"

async function loadPool(): Promise<{
  pool: SandboxPoolStats | null
  error: string | null
}> {
  try {
    const pool = await api.sandbox.pool()
    return { pool, error: null }
  } catch (e) {
    const error =
      e instanceof Error ? e.message : "Sandbox module unreachable"
    return { pool: null, error }
  }
}

export default async function SandboxesPage() {
  const { pool, error } = await loadPool()

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Sandboxes"
        description="Adapter configuration, network policies, and resource defaults for ephemeral compute."
      />
      <SandboxesConfigView pool={pool} error={error} />
    </div>
  )
}
