"use client"

// SkillsView — card grid of installed skills with install / uninstall /
// attach actions.
//
// Each card shows:
//   - name + version (Badge)
//   - description (truncated, with tooltip on hover)
//   - harnesses (chip per entry)
//   - tags (chip per entry)
//   - tenant scope (shared vs single tenant)
//
// Actions:
//   - "Attach" opens a dialog with an agent / harness id input.
//   - "Uninstall" uses an AlertDialog for destructive confirmation.
//   - "Install skill" opens the InstallSkillDialog.
//
// The list endpoint returns { skills: [] } when the runtime has no DB
// wired — we surface that as the empty state with an explanatory
// header, not as an error.

import * as React from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import {
  Boxes,
  Layers,
  Link2,
  MoreHorizontal,
  Plus,
  RefreshCw,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"

import { api, ApiError, type Skill, type SkillList } from "@/lib/api"
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"

import { InstallSkillDialog } from "./install-skill-dialog"

const AttachSchema = z.object({
  agent: z.string().min(1, "Required"),
})
type AttachValues = z.infer<typeof AttachSchema>

export function SkillsView({ initial }: { initial: SkillList }) {
  const [skills, setSkills] = React.useState<Skill[]>(initial.skills)
  // Loading is only true on explicit refresh after the initial SSR
  // hydration — the first render uses the server-fetched list directly.
  const [loading, setLoading] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const [openInstall, setOpenInstall] = React.useState(false)
  const [attachTarget, setAttachTarget] = React.useState<Skill | null>(null)
  const [uninstallTarget, setUninstallTarget] = React.useState<Skill | null>(
    null,
  )

  const fetchSkills = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.skills.list()
      setSkills(list.skills)
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to load skills"
      setError(message)
      setSkills([])
    } finally {
      setLoading(false)
    }
  }, [])

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <span className="text-muted-foreground text-xs">
          {loading
            ? "Loading…"
            : skills.length === 0
              ? "No skills installed"
              : `${skills.length} skill${skills.length === 1 ? "" : "s"} installed`}
        </span>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => void fetchSkills()}
            disabled={loading}
            aria-label="Refresh"
          >
            <RefreshCw />
            Refresh
          </Button>
          <Button size="sm" onClick={() => setOpenInstall(true)}>
            <Plus />
            Install skill
          </Button>
        </div>
      </div>

      {loading && skills.length === 0 ? (
        <SkeletonGrid />
      ) : skills.length === 0 ? (
        <SkillsEmpty
          error={error}
          onInstall={() => setOpenInstall(true)}
        />
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {skills.map((skill) => (
            <SkillCard
              key={skill.id}
              skill={skill}
              onAttach={() => setAttachTarget(skill)}
              onUninstall={() => setUninstallTarget(skill)}
            />
          ))}
        </div>
      )}

      <InstallSkillDialog
        open={openInstall}
        onOpenChange={setOpenInstall}
        onInstalled={fetchSkills}
      />
      <AttachSkillDialog
        target={attachTarget}
        onOpenChange={(open) => !open && setAttachTarget(null)}
      />
      <UninstallSkillDialog
        target={uninstallTarget}
        onOpenChange={(open) => !open && setUninstallTarget(null)}
        onDeleted={fetchSkills}
      />
    </div>
  )
}

function SkillCard({
  skill,
  onAttach,
  onUninstall,
}: {
  skill: Skill
  onAttach: () => void
  onUninstall: () => void
}) {
  const tenantLabel = skill.tenant_id ? "Tenant" : "Shared"
  return (
    <div className="bg-card flex flex-col gap-3 rounded-xl border p-4">
      <div className="flex items-start justify-between gap-2">
        <div className="flex flex-col gap-1">
          <span className="font-medium">{skill.name}</span>
          <span className="text-muted-foreground font-mono text-xs">
            {skill.id}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <Badge variant="secondary">{skill.version}</Badge>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-sm" aria-label="Actions">
                  <MoreHorizontal />
                </Button>
              }
            />
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={onAttach}>
                <Link2 />
                Attach to agent
              </DropdownMenuItem>
              <DropdownMenuItem
                variant="destructive"
                onClick={onUninstall}
              >
                <Trash2 />
                Uninstall
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      <p className="text-muted-foreground line-clamp-2 text-xs">
        {skill.description ?? "No description provided."}
      </p>

      <div className="flex flex-wrap gap-1">
        {skill.harnesses.length === 0 ? (
          <Badge variant="outline" className="text-muted-foreground">
            no harnesses
          </Badge>
        ) : (
          skill.harnesses.map((h) => (
            <Badge key={`h-${h}`} variant="outline">
              {h}
            </Badge>
          ))
        )}
      </div>

      {skill.tags.length > 0 ? (
        <div className="flex flex-wrap gap-1">
          {skill.tags.map((t) => (
            <Badge key={`t-${t}`} variant="ghost" className="text-xs">
              {t}
            </Badge>
          ))}
        </div>
      ) : null}

      <div className="flex items-center justify-between border-t pt-3">
        <span className="text-muted-foreground text-xs">
          {tenantLabel}
          {skill.tenant_id ? `: ${skill.tenant_id}` : ""}
        </span>
        <Button size="sm" variant="outline" onClick={onAttach}>
          <Link2 />
          Attach
        </Button>
      </div>
    </div>
  )
}

function SkeletonGrid() {
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="bg-card flex flex-col gap-3 rounded-xl border p-4">
          <Skeleton className="h-5 w-32" />
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-3/4" />
          <div className="flex gap-2">
            <Skeleton className="h-5 w-16 rounded-4xl" />
            <Skeleton className="h-5 w-12 rounded-4xl" />
          </div>
        </div>
      ))}
    </div>
  )
}

function SkillsEmpty({
  error,
  onInstall,
}: {
  error: string | null
  onInstall: () => void
}) {
  return (
    <Empty>
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Layers />
        </EmptyMedia>
        <EmptyTitle>No skills installed</EmptyTitle>
        <EmptyDescription>
          {error ? (
            <>
              The runtime returned:{" "}
              <code className="font-mono">{error}</code>. Once the skills store
              is available you can install bundles here.
            </>
          ) : (
            "Skills are AF skillkit bundles — prompt overlays + optional tool bindings. Install one and attach it to an agent or harness."
          )}
        </EmptyDescription>
      </EmptyHeader>
      <div className="mt-4 flex justify-center">
        <Button size="sm" onClick={onInstall}>
          <Plus />
          Install skill
        </Button>
      </div>
    </Empty>
  )
}

function AttachSkillDialog({
  target,
  onOpenChange,
}: {
  target: Skill | null
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Attach skill</DialogTitle>
          <DialogDescription>
            Bind{" "}
            <code className="font-mono">{target?.name}</code>{" "}
            <Badge variant="secondary" className="ml-1">
              {target?.version}
            </Badge>{" "}
            onto an agent or harness id.
          </DialogDescription>
        </DialogHeader>
        {target ? (
          <AttachSkillBody
            key={target.id}
            target={target}
            onClose={() => onOpenChange(false)}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function AttachSkillBody({
  target,
  onClose,
}: {
  target: Skill
  onClose: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)
  const form = useForm<AttachValues>({
    resolver: zodResolver(AttachSchema),
    defaultValues: { agent: "" },
    mode: "onBlur",
  })

  const handleSubmit = async (values: AttachValues) => {
    setSubmitting(true)
    try {
      await api.skills.attach({
        skill_id: target.id,
        agent: values.agent,
      })
      toast.success("Skill attached", {
        description: `${target.name} → ${values.agent}`,
      })
      onClose()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to attach skill"
      toast.error("Could not attach skill", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={form.handleSubmit(handleSubmit)}>
      <FieldGroup>
        <Field data-invalid={form.formState.errors.agent ? true : undefined}>
          <FieldLabel htmlFor="attach-agent">Agent or harness</FieldLabel>
          <Input
            id="attach-agent"
            placeholder="acme.review or claude-code"
            autoCorrect="off"
            spellCheck={false}
            aria-invalid={form.formState.errors.agent ? true : undefined}
            {...form.register("agent")}
          />
          {form.formState.errors.agent ? (
            <FieldDescription>
              {form.formState.errors.agent.message}
            </FieldDescription>
          ) : (
            <FieldDescription>
              The AF agent <code className="font-mono">ns.func</code> id, or a
              harness provider id (e.g.{" "}
              <code className="font-mono">claude-code</code>).
            </FieldDescription>
          )}
        </Field>
      </FieldGroup>
      <DialogFooter className="mt-4">
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={onClose}
          disabled={submitting}
        >
          Cancel
        </Button>
        <Button type="submit" size="sm" disabled={submitting}>
          {submitting ? "Attaching…" : "Attach"}
        </Button>
      </DialogFooter>
    </form>
  )
}

function UninstallSkillDialog({
  target,
  onOpenChange,
  onDeleted,
}: {
  target: Skill | null
  onOpenChange: (open: boolean) => void
  onDeleted: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)

  const handleDelete = async () => {
    if (!target) return
    setSubmitting(true)
    try {
      await api.skills.uninstall(target.id)
      toast.success("Skill uninstalled", { description: target.name })
      onOpenChange(false)
      onDeleted()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to uninstall skill"
      toast.error("Could not uninstall", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AlertDialog
      open={target !== null}
      onOpenChange={(open) => {
        if (!open) onOpenChange(false)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Uninstall skill?</AlertDialogTitle>
          <AlertDialogDescription>
            <code className="font-mono">{target?.name}</code> will be removed
            from the runtime and detached from every agent. This cannot be
            undone.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={submitting}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={handleDelete}
            disabled={submitting}
          >
            {submitting ? "Uninstalling…" : "Uninstall"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

// Suppress unused-import warning when these icons are tree-shaken.
export const _ICON_USED = { Boxes }
