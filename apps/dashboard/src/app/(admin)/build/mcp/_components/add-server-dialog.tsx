"use client"

// AddServerDialog — form for registering a new MCP server.
//
// Transport toggles between stdio and sse. Different fields show for
// each: stdio needs a command (split by whitespace into argv); sse needs
// a url. Both can carry an env table and optional description / tenant
// scope.
//
// Env values can reference vault secrets via `secret:<key>` so we expose
// a quick "from secret" picker that fetches the list lazily on focus.

import * as React from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { Plus, X } from "lucide-react"
import { toast } from "sonner"
import { z } from "zod"

import {
  api,
  ApiError,
  type CreateMCPServerInput,
  type MCPTransport,
} from "@/lib/api"
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
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

const NAME_PATTERN = /^[a-z0-9][a-z0-9-]*$/

const AddServerSchema = z.object({
  name: z
    .string()
    .min(1, "Required")
    .max(64, "Too long")
    .refine((v) => NAME_PATTERN.test(v), {
      message: "lowercase letters, digits, hyphens (start with letter/digit)",
    }),
  transport: z.enum(["stdio", "sse"]),
  command: z.string().optional(),
  url: z.string().optional(),
  description: z.string().optional(),
  tenant_id: z.string().optional(),
})
type AddServerValues = z.infer<typeof AddServerSchema>

type EnvRow = { key: string; value: string }

type AddServerDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => void | Promise<void>
}

export function AddServerDialog({
  open,
  onOpenChange,
  onSaved,
}: AddServerDialogProps) {
  const [submitting, setSubmitting] = React.useState(false)
  const [envRows, setEnvRows] = React.useState<EnvRow[]>([])
  const [secretKeys, setSecretKeys] = React.useState<string[] | null>(null)

  const form = useForm<AddServerValues>({
    resolver: zodResolver(AddServerSchema),
    defaultValues: {
      name: "",
      transport: "stdio",
      command: "",
      url: "",
      description: "",
      tenant_id: "",
    },
    mode: "onBlur",
  })

  const transport = form.watch("transport") as MCPTransport

  React.useEffect(() => {
    if (open) {
      form.reset({
        name: "",
        transport: "stdio",
        command: "",
        url: "",
        description: "",
        tenant_id: "",
      })
      setEnvRows([])
      setSecretKeys(null)
    }
  }, [open, form])

  // Lazy-fetch the secrets vault key list. Done once on first focus into
  // any env value field, with toast suppression on failure — the picker
  // is a convenience, not a requirement.
  const fetchSecrets = React.useCallback(async () => {
    if (secretKeys !== null) return
    try {
      const list = await api.secrets.list()
      setSecretKeys(list.secrets.map((s) => s.key))
    } catch {
      // Silent: the operator can still type `secret:foo` manually.
      setSecretKeys([])
    }
  }, [secretKeys])

  const updateEnvRow = (idx: number, patch: Partial<EnvRow>) => {
    setEnvRows((rows) =>
      rows.map((row, i) => (i === idx ? { ...row, ...patch } : row)),
    )
  }

  const removeEnvRow = (idx: number) => {
    setEnvRows((rows) => rows.filter((_, i) => i !== idx))
  }

  const addEnvRow = () => {
    setEnvRows((rows) => [...rows, { key: "", value: "" }])
  }

  const handleSubmit = async (values: AddServerValues) => {
    // Transport-specific validation that zod alone can't cover cleanly.
    if (values.transport === "stdio") {
      if (!values.command || values.command.trim() === "") {
        form.setError("command", { message: "Required for stdio transport" })
        return
      }
    } else {
      if (!values.url || values.url.trim() === "") {
        form.setError("url", { message: "Required for sse transport" })
        return
      }
    }

    const env: Record<string, string> = {}
    for (const row of envRows) {
      const key = row.key.trim()
      if (key === "") continue
      env[key] = row.value
    }

    const payload: CreateMCPServerInput = {
      name: values.name,
      transport: values.transport,
    }
    if (values.transport === "stdio") {
      payload.command = (values.command ?? "")
        .trim()
        .split(/\s+/)
        .filter((p) => p.length > 0)
    } else {
      payload.url = values.url
    }
    if (Object.keys(env).length > 0) payload.env = env
    if (values.description && values.description.trim() !== "") {
      payload.description = values.description
    }
    if (values.tenant_id && values.tenant_id.trim() !== "") {
      payload.tenant_id = values.tenant_id
    }

    setSubmitting(true)
    try {
      await api.mcp.addServer(payload)
      toast.success("MCP server added", { description: values.name })
      onOpenChange(false)
      await onSaved()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to add server"
      toast.error("Could not add server", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Add MCP server</DialogTitle>
          <DialogDescription>
            Register an external Model Context Protocol server. The
            runtime probes it immediately and starts proxying its tools.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <Field data-invalid={form.formState.errors.name ? true : undefined}>
              <FieldLabel htmlFor="mcp-name">Name</FieldLabel>
              <Input
                id="mcp-name"
                placeholder="github"
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
                  Used in tool calls: `tools.call_mcp(&quot;
                  {form.watch("name") || "name"}
                  &quot;, &quot;tool&quot;)`.
                </FieldDescription>
              )}
            </Field>

            <Field>
              <FieldLabel>Transport</FieldLabel>
              <ToggleGroup
                // Single-select ToggleGroup. Base UI types `value` as the
                // multi-select array; the cast keeps the form binding
                // ergonomic without losing runtime correctness — we read
                // back the picked value in `onValueChange`.
                value={[transport] as unknown as readonly string[]}
                onValueChange={(value) => {
                  const next =
                    Array.isArray(value)
                      ? (value[value.length - 1] ?? "stdio")
                      : typeof value === "string"
                        ? value
                        : "stdio"
                  if (next !== "stdio" && next !== "sse") return
                  form.setValue("transport", next)
                  // Clear the irrelevant field's error when switching.
                  form.clearErrors(next === "stdio" ? "url" : "command")
                }}
                className="justify-start gap-1"
              >
                <ToggleGroupItem value="stdio" className="font-mono text-xs">
                  stdio
                </ToggleGroupItem>
                <ToggleGroupItem value="sse" className="font-mono text-xs">
                  sse
                </ToggleGroupItem>
              </ToggleGroup>
              <FieldDescription>
                {transport === "stdio"
                  ? "Runtime spawns a child process and talks JSON-RPC over stdin/stdout."
                  : "Long-lived HTTP+SSE stream against the configured url."}
              </FieldDescription>
            </Field>

            {transport === "stdio" ? (
              <Field
                data-invalid={form.formState.errors.command ? true : undefined}
              >
                <FieldLabel htmlFor="mcp-command">Command</FieldLabel>
                <Input
                  id="mcp-command"
                  placeholder="uvx mcp-server-github"
                  autoCorrect="off"
                  spellCheck={false}
                  aria-invalid={
                    form.formState.errors.command ? true : undefined
                  }
                  {...form.register("command")}
                />
                {form.formState.errors.command ? (
                  <FieldDescription>
                    {form.formState.errors.command.message}
                  </FieldDescription>
                ) : (
                  <FieldDescription>
                    Split on whitespace into argv. The runtime does not
                    invoke a shell.
                  </FieldDescription>
                )}
              </Field>
            ) : (
              <Field
                data-invalid={form.formState.errors.url ? true : undefined}
              >
                <FieldLabel htmlFor="mcp-url">URL</FieldLabel>
                <Input
                  id="mcp-url"
                  placeholder="https://mcp.acme.com/sse"
                  autoCorrect="off"
                  spellCheck={false}
                  aria-invalid={form.formState.errors.url ? true : undefined}
                  {...form.register("url")}
                />
                {form.formState.errors.url ? (
                  <FieldDescription>
                    {form.formState.errors.url.message}
                  </FieldDescription>
                ) : (
                  <FieldDescription>
                    Full SSE endpoint. Must speak the MCP protocol over SSE.
                  </FieldDescription>
                )}
              </Field>
            )}

            <Field>
              <FieldLabel>Environment</FieldLabel>
              <FieldDescription>
                Values prefixed with `secret:` resolve against the secrets
                vault at process spawn time.
              </FieldDescription>
              <div className="mt-2 flex flex-col gap-2">
                {envRows.map((row, idx) => (
                  <EnvRowEditor
                    key={idx}
                    row={row}
                    secretKeys={secretKeys}
                    onFocusFetch={fetchSecrets}
                    onChange={(patch) => updateEnvRow(idx, patch)}
                    onRemove={() => removeEnvRow(idx)}
                  />
                ))}
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={addEnvRow}
                  className="self-start"
                >
                  <Plus />
                  Add variable
                </Button>
              </div>
            </Field>

            <Field>
              <FieldLabel htmlFor="mcp-description">Description</FieldLabel>
              <Input
                id="mcp-description"
                placeholder="What this server is for"
                {...form.register("description")}
              />
              <FieldDescription>
                Optional. Shown in the dashboard alongside the server name.
              </FieldDescription>
            </Field>

            <Field>
              <FieldLabel htmlFor="mcp-tenant">Tenant scope</FieldLabel>
              <Input
                id="mcp-tenant"
                placeholder="Leave empty to share across tenants"
                autoCorrect="off"
                spellCheck={false}
                {...form.register("tenant_id")}
              />
              <FieldDescription>
                When MT is on, restricts visibility to a single tenant id.
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
              {submitting ? "Adding…" : "Add server"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ─── env row editor ───────────────────────────────────────────────────────

type EnvRowEditorProps = {
  row: EnvRow
  secretKeys: string[] | null
  onFocusFetch: () => void
  onChange: (patch: Partial<EnvRow>) => void
  onRemove: () => void
}

function EnvRowEditor({
  row,
  secretKeys,
  onFocusFetch,
  onChange,
  onRemove,
}: EnvRowEditorProps) {
  return (
    <div className="grid grid-cols-[1fr_1fr_auto_auto] items-center gap-2">
      <Input
        value={row.key}
        onChange={(e) => onChange({ key: e.target.value })}
        placeholder="KEY"
        autoCapitalize="characters"
        autoCorrect="off"
        spellCheck={false}
        className="font-mono text-xs"
      />
      <Input
        value={row.value}
        onChange={(e) => onChange({ value: e.target.value })}
        onFocus={onFocusFetch}
        placeholder="value or secret:vault_key"
        autoCorrect="off"
        spellCheck={false}
        className="font-mono text-xs"
      />
      <Select
        value=""
        onOpenChange={(open) => {
          if (open) onFocusFetch()
        }}
        onValueChange={(secretKey) => {
          if (typeof secretKey === "string" && secretKey.length > 0) {
            onChange({ value: `secret:${secretKey}` })
          }
        }}
      >
        <SelectTrigger size="sm" className="w-32">
          <SelectValue placeholder="From secret" />
        </SelectTrigger>
        <SelectContent>
          {secretKeys === null || secretKeys.length === 0 ? (
            <SelectItem value="__none__" disabled>
              {secretKeys === null ? "Loading…" : "No secrets stored"}
            </SelectItem>
          ) : (
            secretKeys.map((key) => (
              <SelectItem key={key} value={key}>
                {key}
              </SelectItem>
            ))
          )}
        </SelectContent>
      </Select>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        onClick={onRemove}
        aria-label="Remove variable"
      >
        <X />
      </Button>
    </div>
  )
}
