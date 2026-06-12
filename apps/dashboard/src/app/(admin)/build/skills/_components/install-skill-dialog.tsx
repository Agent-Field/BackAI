"use client"

// InstallSkillDialog — the form that hits POST /api/v1/skills.
//
// Source string format help text mirrors the three formats accepted by
// the runtime's installer:
//
//   - af-skill://<vendor>/<name>@<version>   (registry URL)
//   - /abs/path or ./relative                (local skill.toml/json)
//   - embedded:<name>                        (runtime-bundled bundle)
//
// tenant_id is optional — when omitted the runtime installs as a
// shared bundle visible to every tenant.

import * as React from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"

import { api, ApiError } from "@/lib/api"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"

const InstallSchema = z.object({
  source: z.string().min(1, "Required"),
  tenantId: z.string().optional(),
})
type InstallValues = z.infer<typeof InstallSchema>

export function InstallSkillDialog({
  open,
  onOpenChange,
  onInstalled,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onInstalled: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)
  const form = useForm<InstallValues>({
    resolver: zodResolver(InstallSchema),
    defaultValues: { source: "", tenantId: "" },
    mode: "onBlur",
  })

  React.useEffect(() => {
    if (open) {
      form.reset({ source: "", tenantId: "" })
    }
  }, [open, form])

  const handleSubmit = async (values: InstallValues) => {
    setSubmitting(true)
    try {
      const result = await api.skills.install({
        source: values.source,
        tenant_id:
          values.tenantId && values.tenantId.length > 0
            ? values.tenantId
            : undefined,
      })
      toast.success("Skill installed", {
        description: `${result.name} ${result.version}`,
      })
      onOpenChange(false)
      onInstalled()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to install skill"
      toast.error("Could not install skill", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Install skill</DialogTitle>
          <DialogDescription>
            The runtime reads the manifest, persists it as a row in
            suite_skills, and makes it available for attach.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <Field
              data-invalid={form.formState.errors.source ? true : undefined}
            >
              <FieldLabel htmlFor="install-skill-source">Source</FieldLabel>
              <Input
                id="install-skill-source"
                placeholder="embedded:default"
                autoCorrect="off"
                spellCheck={false}
                aria-invalid={
                  form.formState.errors.source ? true : undefined
                }
                {...form.register("source")}
              />
              {form.formState.errors.source ? (
                <FieldDescription>
                  {form.formState.errors.source.message}
                </FieldDescription>
              ) : (
                <FieldDescription>
                  Accepts three formats: a registry URL
                  (
                  <code className="font-mono">
                    af-skill://&lt;vendor&gt;/&lt;name&gt;@&lt;version&gt;
                  </code>
                  ), a local path (<code className="font-mono">/abs/path</code>
                  {" "}or{" "}<code className="font-mono">./relative</code>), or
                  a built-in bundle (
                  <code className="font-mono">embedded:default</code>).
                </FieldDescription>
              )}
            </Field>
            <Field>
              <FieldLabel htmlFor="install-skill-tenant">
                Tenant ID (optional)
              </FieldLabel>
              <Input
                id="install-skill-tenant"
                placeholder="leave empty for a shared bundle"
                autoCorrect="off"
                spellCheck={false}
                {...form.register("tenantId")}
              />
              <FieldDescription>
                Scope the install to a single tenant. Empty = visible to
                every tenant on this runtime.
              </FieldDescription>
            </Field>
          </FieldGroup>
          <DialogFooter className="mt-4">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => onOpenChange(false)}
              disabled={submitting}
            >
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={submitting}>
              {submitting ? "Installing…" : "Install"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
