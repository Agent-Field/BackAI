"use client"

// CreateCronDialog — form for POST /api/v1/crons.
//
// The job_name select is populated from /api/v1/jobs/definitions; when
// no definitions are returned (no jobs manager yet, or no registrations)
// the field falls back to a free-text Input so the operator isn't
// blocked. The args field is a JSON textarea — we validate locally with
// JSON.parse before submission to give an immediate error rather than
// surfacing the runtime's parse failure.

import * as React from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"

import { api, ApiError, type Cron, type JobDefinition } from "@/lib/api"
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"

const CronSchema = z.object({
  name: z.string().min(1, "Required"),
  jobName: z.string().min(1, "Required"),
  schedule: z.string().min(1, "Required"),
  args: z.string().refine(
    (value) => {
      const trimmed = value.trim()
      if (trimmed === "") return true
      try {
        const parsed = JSON.parse(trimmed)
        return typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)
      } catch {
        return false
      }
    },
    { message: "Must be a JSON object (or empty)" },
  ),
  isActive: z.boolean(),
})
type CronValues = z.infer<typeof CronSchema>

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (cron: Cron) => void
  definitions: JobDefinition[]
}

export function CreateCronDialog({
  open,
  onOpenChange,
  onCreated,
  definitions,
}: Props) {
  const [submitting, setSubmitting] = React.useState(false)
  const form = useForm<CronValues>({
    resolver: zodResolver(CronSchema),
    defaultValues: {
      name: "",
      jobName: "",
      schedule: "@hourly",
      args: "{}",
      isActive: true,
    },
    mode: "onBlur",
  })

  React.useEffect(() => {
    if (open) {
      form.reset({
        name: "",
        jobName: "",
        schedule: "@hourly",
        args: "{}",
        isActive: true,
      })
    }
  }, [open, form])

  const handleSubmit = async (values: CronValues) => {
    setSubmitting(true)
    try {
      const argsRaw = values.args.trim()
      const args = argsRaw === "" ? {} : (JSON.parse(argsRaw) as Record<string, unknown>)
      const result = await api.crons.create({
        name: values.name,
        job_name: values.jobName,
        schedule: values.schedule,
        args,
        is_active: values.isActive,
      })
      toast.success("Cron created", { description: result.name })
      onOpenChange(false)
      onCreated(result)
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to create cron"
      toast.error("Could not create cron", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  const hasDefinitions = definitions.length > 0
  const selectedJob = form.watch("jobName")

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Create cron</DialogTitle>
          <DialogDescription>
            The scheduler will fire the selected job at the configured cadence.
            Schedules use 5-field crontab syntax or the
            <code className="bg-muted mx-1 rounded px-1 font-mono text-xs">
              @hourly
            </code>{" "}
            /
            <code className="bg-muted mx-1 rounded px-1 font-mono text-xs">
              @daily
            </code>{" "}
            shortcuts.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <Field data-invalid={form.formState.errors.name ? true : undefined}>
              <FieldLabel htmlFor="cron-name">Name</FieldLabel>
              <Input
                id="cron-name"
                placeholder="Nightly cleanup"
                autoCorrect="off"
                spellCheck={false}
                aria-invalid={form.formState.errors.name ? true : undefined}
                {...form.register("name")}
              />
              {form.formState.errors.name ? (
                <FieldDescription>
                  {form.formState.errors.name.message}
                </FieldDescription>
              ) : (
                <FieldDescription>
                  A human-readable label shown in the dashboard.
                </FieldDescription>
              )}
            </Field>

            <Field
              data-invalid={
                form.formState.errors.jobName ? true : undefined
              }
            >
              <FieldLabel htmlFor="cron-job">Job</FieldLabel>
              {hasDefinitions ? (
                <Select
                  value={selectedJob}
                  onValueChange={(v) =>
                    form.setValue("jobName", v ?? "", {
                      shouldValidate: true,
                    })
                  }
                >
                  <SelectTrigger id="cron-job">
                    <SelectValue placeholder="Pick a job" />
                  </SelectTrigger>
                  <SelectContent>
                    {definitions.map((d) => (
                      <SelectItem key={d.name} value={d.name}>
                        <span className="font-mono text-xs">{d.name}</span>
                        {d.description ? (
                          <span className="text-muted-foreground ml-2 text-xs">
                            {d.description}
                          </span>
                        ) : null}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <Input
                  id="cron-job"
                  placeholder="sample-job"
                  autoCorrect="off"
                  spellCheck={false}
                  aria-invalid={
                    form.formState.errors.jobName ? true : undefined
                  }
                  {...form.register("jobName")}
                />
              )}
              {form.formState.errors.jobName ? (
                <FieldDescription>
                  {form.formState.errors.jobName.message}
                </FieldDescription>
              ) : (
                <FieldDescription>
                  {hasDefinitions
                    ? "Choose a registered job. Definitions live in apps/backend/jobs/."
                    : "No job definitions registered yet — enter the kind name manually."}
                </FieldDescription>
              )}
            </Field>

            <Field
              data-invalid={
                form.formState.errors.schedule ? true : undefined
              }
            >
              <FieldLabel htmlFor="cron-schedule">Schedule</FieldLabel>
              <Input
                id="cron-schedule"
                placeholder="@hourly"
                autoCorrect="off"
                spellCheck={false}
                className="font-mono"
                aria-invalid={
                  form.formState.errors.schedule ? true : undefined
                }
                {...form.register("schedule")}
              />
              {form.formState.errors.schedule ? (
                <FieldDescription>
                  {form.formState.errors.schedule.message}
                </FieldDescription>
              ) : (
                <FieldDescription>
                  Either a shortcut (
                  <code className="font-mono">@hourly</code>,{" "}
                  <code className="font-mono">@daily</code>,{" "}
                  <code className="font-mono">@weekly</code>,{" "}
                  <code className="font-mono">@monthly</code>) or a standard
                  5-field cron expression (
                  <code className="font-mono">m h dom mon dow</code>). The
                  runtime rejects sub-minute resolutions.
                </FieldDescription>
              )}
            </Field>

            <Field data-invalid={form.formState.errors.args ? true : undefined}>
              <FieldLabel htmlFor="cron-args">Args</FieldLabel>
              <Textarea
                id="cron-args"
                placeholder='{}'
                className="font-mono text-xs"
                rows={4}
                aria-invalid={form.formState.errors.args ? true : undefined}
                {...form.register("args")}
              />
              {form.formState.errors.args ? (
                <FieldDescription>
                  {form.formState.errors.args.message}
                </FieldDescription>
              ) : (
                <FieldDescription>
                  JSON object passed verbatim to the job on every tick. Leave
                  empty for no args.
                </FieldDescription>
              )}
            </Field>

            <Field>
              <div className="flex items-center justify-between">
                <FieldLabel htmlFor="cron-active">Active</FieldLabel>
                <Switch
                  id="cron-active"
                  checked={form.watch("isActive")}
                  onCheckedChange={(checked) =>
                    form.setValue("isActive", checked)
                  }
                />
              </div>
              <FieldDescription>
                Inactive crons stay in the table but don&apos;t fire — useful
                for pausing a noisy schedule without losing its config.
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
              {submitting ? "Creating…" : "Create"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
