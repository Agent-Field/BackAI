// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import { api } from "@/lib/api"
import type { TenantDrilldown } from "@/lib/api"

// Settings tab — identity edit + Danger zone (delete with
// type-to-confirm). Suspend / Reactivate live in the header so the
// operator can act without first navigating into a tab.

interface SettingsTabProps {
  drilldown: TenantDrilldown
  onMutated: () => Promise<void> | void
}

export function SettingsTab({ drilldown, onMutated }: SettingsTabProps) {
  return (
    <div className="flex flex-col gap-section">
      <IdentityCard drilldown={drilldown} onMutated={onMutated} />
      <DangerZone drilldown={drilldown} onMutated={onMutated} />
    </div>
  )
}

function IdentityCard({
  drilldown,
  onMutated,
}: {
  drilldown: TenantDrilldown
  onMutated: () => Promise<void> | void
}) {
  const [name, setName] = useState(drilldown.tenant.name ?? "")
  const [saving, setSaving] = useState(false)
  const changed = name !== (drilldown.tenant.name ?? "")
  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!changed || saving) return
    setSaving(true)
    try {
      await api.admin.tenants.update(drilldown.tenant.id, { name })
      toast.success("Tenant updated", { description: name })
      await onMutated()
    } catch (err) {
      toast.error("Could not update tenant", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setSaving(false)
    }
  }
  return (
    <ZoneCard>
      <ZoneCardHeader title="Identity" />
      <form onSubmit={onSubmit} className="flex flex-col gap-stack p-row-x">
        <label className="flex flex-col gap-tile-tight">
          <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
            Display name
          </span>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="acme corporation"
            className="text-meta"
          />
        </label>
        <div className="grid grid-cols-2 gap-stack text-meta">
          <Field
            label="Slug"
            value={drilldown.tenant.slug}
            sublabel="read-only"
          />
          <Field
            label="Plan"
            value={drilldown.tenant.plan ?? "—"}
            sublabel="managed by Platform → Billing"
          />
        </div>
        <div className="flex justify-end">
          <Button
            type="submit"
            size="sm"
            className="h-7 gap-inline text-meta"
            disabled={!changed || saving}
          >
            {saving ? "Saving…" : "Save changes"}
          </Button>
        </div>
      </form>
    </ZoneCard>
  )
}

function Field({
  label,
  value,
  sublabel,
}: {
  label: string
  value: string
  sublabel?: string
}) {
  return (
    <div className="flex min-w-0 flex-col gap-tile-tight">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <span className="font-mono text-meta text-foreground">{value}</span>
      {sublabel ? (
        <span className="text-meta text-muted-foreground">{sublabel}</span>
      ) : null}
    </div>
  )
}

function DangerZone({
  drilldown,
  onMutated,
}: {
  drilldown: TenantDrilldown
  onMutated: () => Promise<void> | void
}) {
  const tenant = drilldown.tenant
  const [confirm, setConfirm] = useState("")
  const [deleting, setDeleting] = useState(false)
  const expected = tenant.name || tenant.slug
  const canDelete = confirm === expected && !deleting

  const onDelete = async () => {
    if (!canDelete) return
    setDeleting(true)
    try {
      await api.admin.tenants.delete(tenant.id)
      toast.success("Tenant deleted", { description: tenant.name })
      await onMutated()
    } catch (err) {
      toast.error("Could not delete tenant", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setDeleting(false)
    }
  }

  return (
    <ZoneCard className="border-destructive/40 bg-destructive/5">
      <ZoneCardHeader
        title="Danger zone"
        subtitle="Destructive operations"
      />
      <div className="flex flex-col gap-stack p-row-x">
        <p className="text-meta text-muted-foreground">
          Deleting a tenant cascades to its keys, members, runs, and
          audit log. This is permanent. Type{" "}
          <span className="font-mono text-foreground">{expected}</span>{" "}
          to confirm.
        </p>
        <Input
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          placeholder={expected}
          className="font-mono text-meta"
        />
        <div className="flex justify-end">
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-7 gap-inline border-destructive/60 text-meta text-destructive hover:bg-destructive/10 hover:text-destructive"
            disabled={!canDelete}
            onClick={onDelete}
          >
            {deleting ? "Deleting…" : "Delete tenant"}
          </Button>
        </div>
      </div>
    </ZoneCard>
  )
}
