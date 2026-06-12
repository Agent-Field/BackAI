"use client"

// SqlRunnerTab — ad-hoc SQL editor.
//
// The read-only switch is enforced by the runtime; the client side just
// asserts it so the user sees what mode they're in. ⌘+Enter (or Ctrl+Enter
// on Linux/Windows) submits the query.

import * as React from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { AlertCircle, Database, Play } from "lucide-react"
import { toast } from "sonner"

import { api, ApiError, type SQLRunResult } from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"

import { stringifyValue, truncate } from "../_lib/format"

type RunState =
  | { kind: "idle" }
  | { kind: "running" }
  | { kind: "result"; result: SQLRunResult }
  | { kind: "error"; code: string; message: string }

const DEFAULT_LIMIT = 500

export function SqlRunnerTab() {
  const [statement, setStatement] = React.useState(
    "SELECT now() AS server_time;",
  )
  const [readOnly, setReadOnly] = React.useState(true)
  const [state, setState] = React.useState<RunState>({ kind: "idle" })

  const submit = React.useCallback(async () => {
    const trimmed = statement.trim()
    if (!trimmed) {
      toast.warning("Enter a SQL statement to run")
      return
    }
    setState({ kind: "running" })
    try {
      const result = await api.db.sql({
        statement: trimmed,
        read_only: readOnly,
        limit: DEFAULT_LIMIT,
      })
      setState({ kind: "result", result })
    } catch (e) {
      if (e instanceof ApiError) {
        setState({ kind: "error", code: e.code, message: e.message })
      } else {
        setState({
          kind: "error",
          code: "UNKNOWN",
          message: e instanceof Error ? e.message : "Failed to run SQL",
        })
      }
    }
  }, [statement, readOnly])

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault()
      void submit()
    }
  }

  const running = state.kind === "running"

  return (
    <div className="flex flex-col gap-4">
      <Card className="gap-3">
        <CardHeader>
          <CardTitle>SQL Runner</CardTitle>
          <CardDescription>
            Run ad-hoc queries against the embedded database. Use{" "}
            <kbd className="bg-muted rounded px-1 py-0.5 font-mono text-[10px]">
              ⌘
            </kbd>{" "}
            +{" "}
            <kbd className="bg-muted rounded px-1 py-0.5 font-mono text-[10px]">
              Enter
            </kbd>{" "}
            to run.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Textarea
            value={statement}
            onChange={(e) => setStatement(e.target.value)}
            onKeyDown={handleKeyDown}
            rows={10}
            spellCheck={false}
            autoCorrect="off"
            autoCapitalize="off"
            className="font-mono text-xs"
            placeholder="SELECT * FROM …"
          />
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <TooltipProvider delay={300}>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <label className="flex items-center gap-2 text-xs">
                        <Switch
                          checked={readOnly}
                          onCheckedChange={setReadOnly}
                        />
                        <span>Read-only</span>
                      </label>
                    }
                  />
                  <TooltipContent>
                    Enforced at the runtime. Disable to allow INSERT/UPDATE/DELETE
                    and DDL.
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
              {!readOnly ? (
                <Badge variant="destructive">writes enabled</Badge>
              ) : null}
            </div>
            <Button size="sm" onClick={() => void submit()} disabled={running}>
              <Play />
              {running ? "Running…" : "Run"}
            </Button>
          </div>
        </CardContent>
      </Card>

      <ResultPanel state={state} />
    </div>
  )
}

function ResultPanel({ state }: { state: RunState }) {
  if (state.kind === "idle") {
    return (
      <Empty className="border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <Database />
          </EmptyMedia>
          <EmptyTitle>No results yet</EmptyTitle>
          <EmptyDescription>
            Write a SQL statement above and run it to see results.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  if (state.kind === "running") {
    return (
      <Empty className="border-dashed">
        <EmptyHeader>
          <EmptyTitle>Running…</EmptyTitle>
          <EmptyDescription>
            Waiting for the runtime to return.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  if (state.kind === "error") {
    return (
      <Alert variant="destructive">
        <AlertCircle />
        <AlertTitle className="flex items-center gap-2">
          Query failed
          <Badge variant="destructive" className="font-mono">
            {state.code}
          </Badge>
        </AlertTitle>
        <AlertDescription>
          <pre className="overflow-x-auto font-mono text-xs whitespace-pre-wrap">
            {state.message}
          </pre>
        </AlertDescription>
      </Alert>
    )
  }

  return <ResultTable result={state.result} />
}

function ResultTable({ result }: { result: SQLRunResult }) {
  const columns = React.useMemo<ColumnDef<unknown[]>[]>(() => {
    return result.columns.map((name, idx) => ({
      id: name,
      header: name,
      cell: ({ row }) => {
        const v = row.original[idx]
        if (v === null) {
          return <span className="text-muted-foreground italic">null</span>
        }
        const text = stringifyValue(v)
        return <span title={text}>{truncate(text, 80)}</span>
      },
    }))
  }, [result.columns])

  const tableInstance = useReactTable({
    data: result.rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <Card className="gap-0">
      <CardHeader className="border-b pb-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-col gap-0.5">
            <CardTitle>Result</CardTitle>
            <CardDescription>
              {result.row_count.toLocaleString()} row
              {result.row_count === 1 ? "" : "s"} · {result.duration_ms}ms
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            {result.truncated ? (
              <Badge variant="secondary">truncated</Badge>
            ) : null}
            <Badge variant="outline" className="tabular-nums">
              {result.columns.length} cols
            </Badge>
          </div>
        </div>
      </CardHeader>
      <CardContent className="p-0">
        {result.rows.length === 0 ? (
          <Empty className="border-0">
            <EmptyHeader>
              <EmptyTitle>No rows returned</EmptyTitle>
              <EmptyDescription>
                The query succeeded but returned an empty result set.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <ScrollArea className="max-h-[480px]">
            <Table>
              <TableHeader>
                {tableInstance.getHeaderGroups().map((hg) => (
                  <TableRow key={hg.id} className="hover:bg-transparent">
                    {hg.headers.map((header) => (
                      <TableHead
                        key={header.id}
                        className="text-muted-foreground text-xs font-medium uppercase tracking-wide whitespace-nowrap"
                      >
                        {header.isPlaceholder
                          ? null
                          : flexRender(
                              header.column.columnDef.header,
                              header.getContext(),
                            )}
                      </TableHead>
                    ))}
                  </TableRow>
                ))}
              </TableHeader>
              <TableBody>
                {tableInstance.getRowModel().rows.map((row) => (
                  <TableRow key={row.id} className="hover:bg-muted/30">
                    {row.getVisibleCells().map((cell) => (
                      <TableCell
                        key={cell.id}
                        className="font-mono text-xs whitespace-nowrap"
                      >
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext(),
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </ScrollArea>
        )}
      </CardContent>
    </Card>
  )
}
