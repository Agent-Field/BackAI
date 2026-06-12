"use client"

// CallToolSheet — side panel for invoking an arbitrary MCP tool.
//
// Operators pick a server, then a tool, paste a JSON arguments object,
// and click Call. The runtime forwards the call and returns the typed
// content parts + duration; we render them with the error flag prominent
// so a bad call is immediately visible.

import * as React from "react"
import { CheckCircle2, Loader2, Play, XCircle } from "lucide-react"

import {
  api,
  ApiError,
  type CallMCPResult,
  type MCPServer,
  type MCPTool,
} from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Textarea } from "@/components/ui/textarea"

type CallToolSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  servers: MCPServer[]
  tools: MCPTool[]
  initialServer?: string
}

type CallState =
  | { kind: "idle" }
  | { kind: "calling" }
  | { kind: "ok"; result: CallMCPResult }
  | { kind: "error"; message: string }

export function CallToolSheet({
  open,
  onOpenChange,
  servers,
  tools,
  initialServer,
}: CallToolSheetProps) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="flex w-full flex-col gap-0 p-0 sm:max-w-xl">
        <CallToolBody
          key={open ? "open" : "closed"}
          servers={servers}
          tools={tools}
          initialServer={initialServer}
        />
      </SheetContent>
    </Sheet>
  )
}

function CallToolBody({
  servers,
  tools,
  initialServer,
}: {
  servers: MCPServer[]
  tools: MCPTool[]
  initialServer?: string
}) {
  const initialServerFallback =
    initialServer ?? servers.find((s) => s.is_enabled)?.name ?? null
  const [server, setServer] = React.useState<string | null>(
    initialServerFallback,
  )
  const initialToolFallback = React.useMemo(() => {
    if (server === null) return null
    const first = tools.find((t) => t.server === server)
    return first ? first.name : null
  }, [server, tools])
  const [tool, setTool] = React.useState<string | null>(initialToolFallback)
  const [argsText, setArgsText] = React.useState("{}")
  const [state, setState] = React.useState<CallState>({ kind: "idle" })

  // When the server changes, reset the tool selection to the first tool
  // available on that server (or null when none).
  React.useEffect(() => {
    if (server === null) {
      setTool(null)
      return
    }
    setTool((current) => {
      if (current !== null && tools.some((t) => t.server === server && t.name === current)) {
        return current
      }
      const first = tools.find((t) => t.server === server)
      return first ? first.name : null
    })
  }, [server, tools])

  const toolsForServer = tools.filter((t) => t.server === server)
  const selectedTool = toolsForServer.find((t) => t.name === tool) ?? null

  const handleCall = async () => {
    if (server === null || tool === null) return
    let parsed: Record<string, unknown> | undefined
    if (argsText.trim() === "") {
      parsed = undefined
    } else {
      try {
        const candidate = JSON.parse(argsText)
        if (typeof candidate !== "object" || candidate === null || Array.isArray(candidate)) {
          setState({
            kind: "error",
            message: "Arguments must be a JSON object.",
          })
          return
        }
        parsed = candidate as Record<string, unknown>
      } catch (e) {
        setState({
          kind: "error",
          message:
            e instanceof Error
              ? `Invalid JSON: ${e.message}`
              : "Invalid JSON arguments",
        })
        return
      }
    }

    setState({ kind: "calling" })
    try {
      const result = await api.mcp.call({
        server,
        tool,
        arguments: parsed,
      })
      setState({ kind: "ok", result })
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Tool call failed"
      setState({ kind: "error", message })
    }
  }

  return (
    <>
      <SheetHeader>
        <SheetTitle>Call MCP tool</SheetTitle>
        <SheetDescription>
          Invoke an MCP tool with a JSON arguments object. The runtime
          forwards the call to the configured server and returns the
          typed content parts.
        </SheetDescription>
      </SheetHeader>
      <ScrollArea className="flex-1">
        <div className="flex flex-col gap-4 p-4">
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="call-server">Server</FieldLabel>
              <Select
                value={server ?? ""}
                onValueChange={(value) =>
                  setServer(typeof value === "string" ? value : null)
                }
              >
                <SelectTrigger id="call-server" size="sm">
                  <SelectValue placeholder="Select a server" />
                </SelectTrigger>
                <SelectContent>
                  {servers.length === 0 ? (
                    <SelectItem value="__none__" disabled>
                      No servers configured
                    </SelectItem>
                  ) : (
                    servers.map((s) => (
                      <SelectItem key={s.name} value={s.name}>
                        {s.name}
                      </SelectItem>
                    ))
                  )}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="call-tool">Tool</FieldLabel>
              <Select
                value={tool ?? ""}
                onValueChange={(value) =>
                  setTool(typeof value === "string" ? value : null)
                }
                disabled={server === null || toolsForServer.length === 0}
              >
                <SelectTrigger id="call-tool" size="sm">
                  <SelectValue
                    placeholder={
                      server === null
                        ? "Pick a server first"
                        : toolsForServer.length === 0
                          ? "No tools available"
                          : "Select a tool"
                    }
                  />
                </SelectTrigger>
                <SelectContent>
                  {toolsForServer.map((t) => (
                    <SelectItem key={t.id} value={t.name}>
                      {t.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {selectedTool?.description ? (
                <FieldDescription>{selectedTool.description}</FieldDescription>
              ) : null}
            </Field>
            <Field>
              <FieldLabel htmlFor="call-args">Arguments</FieldLabel>
              <Textarea
                id="call-args"
                value={argsText}
                onChange={(e) => setArgsText(e.target.value)}
                rows={8}
                spellCheck={false}
                autoCorrect="off"
                className="font-mono text-xs"
                placeholder='{"q": "agentfield"}'
              />
              <FieldDescription>
                JSON object matching the tool&apos;s input schema. Use{" "}
                <span className="font-mono">{`{}`}</span> for no-arg
                tools.
              </FieldDescription>
            </Field>
          </FieldGroup>

          {selectedTool ? (
            <div className="rounded-md border bg-card p-3">
              <div className="text-muted-foreground mb-2 text-[10px] font-medium uppercase tracking-wide">
                Input schema
              </div>
              <pre className="bg-muted max-h-48 overflow-auto rounded-md p-2 font-mono text-[11px] leading-snug">
                {JSON.stringify(selectedTool.input_schema, null, 2)}
              </pre>
            </div>
          ) : null}

          <ResultPanel state={state} />
        </div>
      </ScrollArea>
      <div className="flex items-center justify-end border-t p-3">
        <Button
          size="sm"
          onClick={() => void handleCall()}
          disabled={
            server === null || tool === null || state.kind === "calling"
          }
        >
          {state.kind === "calling" ? (
            <>
              <Loader2 className="animate-spin" />
              Calling…
            </>
          ) : (
            <>
              <Play />
              Call
            </>
          )}
        </Button>
      </div>
    </>
  )
}

function ResultPanel({ state }: { state: CallState }) {
  if (state.kind === "idle") {
    return (
      <Empty className="border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <Play />
          </EmptyMedia>
          <EmptyTitle>No result yet</EmptyTitle>
          <EmptyDescription>
            Fill in the form above and press Call to invoke the tool.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  if (state.kind === "calling") {
    return (
      <div className="text-muted-foreground flex items-center gap-2 rounded-md border bg-card p-3 text-xs">
        <Loader2 className="animate-spin size-3.5" />
        Calling the server…
      </div>
    )
  }

  if (state.kind === "error") {
    return (
      <div className="bg-destructive/10 text-destructive flex items-start gap-2 rounded-md p-3 text-xs">
        <XCircle className="mt-0.5 size-3.5 shrink-0" />
        <span className="font-mono">{state.message}</span>
      </div>
    )
  }

  const { result } = state
  return (
    <div className="flex flex-col gap-2 rounded-md border bg-card p-3">
      <div className="flex items-center gap-2">
        {result.is_error ? (
          <Badge variant="destructive" className="gap-1">
            <XCircle className="size-3" />
            Tool error
          </Badge>
        ) : (
          <Badge variant="default" className="gap-1">
            <CheckCircle2 className="size-3" />
            Success
          </Badge>
        )}
        <span className="text-muted-foreground text-xs tabular-nums">
          {result.duration_ms} ms
        </span>
      </div>
      {result.content.length === 0 ? (
        <p className="text-muted-foreground text-xs">
          The tool returned no content parts.
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {result.content.map((part, idx) => (
            <li
              key={idx}
              className="rounded-md border bg-background p-2 text-xs"
            >
              <div className="text-muted-foreground mb-1 text-[10px] font-medium uppercase tracking-wide">
                {String(part.type ?? "part")} · #{idx + 1}
              </div>
              <pre className="bg-muted max-h-48 overflow-auto rounded-md p-2 font-mono text-[11px] leading-snug">
                {JSON.stringify(part, null, 2)}
              </pre>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
