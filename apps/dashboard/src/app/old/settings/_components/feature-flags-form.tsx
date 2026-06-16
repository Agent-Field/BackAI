// SPDX-License-Identifier: Apache-2.0

"use client"

import * as React from "react"

import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { api, type FeatureFlag } from "@/lib/api"

export function FeatureFlagsForm() {
  const [flags, setFlags] = React.useState<FeatureFlag[]>([])
  const [loading, setLoading] = React.useState(true)
  const [savingKey, setSavingKey] = React.useState<string | null>(null)
  const [error, setError] = React.useState<string | null>(null)

  React.useEffect(() => {
    let cancelled = false
    setLoading(true)
    api.config.flags
      .list()
      .then((result) => {
        if (cancelled) return
        setFlags(result.flags)
        setError(null)
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : "Failed to load feature flags")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  async function setFlag(flag: FeatureFlag, enabled: boolean) {
    const previous = flags
    setSavingKey(flag.key)
    setError(null)
    setFlags((current) =>
      current.map((item) => (item.key === flag.key ? { ...item, enabled } : item)),
    )
    try {
      const next = await api.config.flags.set(flag.key, { enabled })
      setFlags((current) => current.map((item) => (item.key === next.key ? next : item)))
    } catch (err) {
      setFlags(previous)
      setError(err instanceof Error ? err.message : "Failed to update feature flag")
    } finally {
      setSavingKey(null)
    }
  }

  if (loading) {
    return (
      <FieldGroup>
        {[0, 1, 2].map((idx) => (
          <div key={idx} className="flex items-center justify-between gap-4 py-2">
            <div className="flex flex-1 flex-col gap-2">
              <Skeleton className="h-4 w-48" />
              <Skeleton className="h-3 w-full max-w-md" />
            </div>
            <Skeleton className="h-6 w-10" />
          </div>
        ))}
      </FieldGroup>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {error ? <p className="text-destructive text-sm">{error}</p> : null}
      <FieldGroup>
        {flags.map((flag) => (
          <Field key={flag.key} orientation="horizontal">
            <div className="flex flex-1 flex-col gap-0.5">
              <FieldLabel htmlFor={flag.key}>{flag.label || flag.key}</FieldLabel>
              <FieldDescription>{flag.description}</FieldDescription>
            </div>
            <Switch
              id={flag.key}
              checked={flag.enabled}
              disabled={savingKey === flag.key}
              onCheckedChange={(next) => void setFlag(flag, next)}
            />
          </Field>
        ))}
      </FieldGroup>
    </div>
  )
}
