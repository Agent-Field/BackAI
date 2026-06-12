"use client"

// TableSidebar — searchable, schema-grouped list of tables in the database.
//
// Click selects a table; the studio's main pane keys off the selection.
// Badges show RLS state and approximate row count so operators can spot
// large or RLS-gated tables at a glance.

import * as React from "react"
import { Database, RotateCw, SearchIcon, ShieldCheck } from "lucide-react"

import type { DBTable } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

import { formatRowCount } from "../_lib/format"
import type { SelectedTable } from "./database-studio"

type TableSidebarProps = {
  tables: DBTable[]
  loading: boolean
  error: string | null
  selected: SelectedTable | null
  onSelect: (t: SelectedTable) => void
  onRefresh: () => void
}

export function TableSidebar({
  tables,
  loading,
  error,
  selected,
  onSelect,
  onRefresh,
}: TableSidebarProps) {
  const [query, setQuery] = React.useState("")

  const grouped = React.useMemo(() => {
    const q = query.trim().toLowerCase()
    const filtered = q
      ? tables.filter(
          (t) =>
            t.name.toLowerCase().includes(q) ||
            t.schema.toLowerCase().includes(q),
        )
      : tables
    const map = new Map<string, DBTable[]>()
    for (const t of filtered) {
      const arr = map.get(t.schema) ?? []
      arr.push(t)
      map.set(t.schema, arr)
    }
    // Sort schemas alphabetically; "public" first if present.
    const schemas = Array.from(map.keys()).sort((a, b) => {
      if (a === "public") return -1
      if (b === "public") return 1
      return a.localeCompare(b)
    })
    for (const s of schemas) {
      map.get(s)?.sort((a, b) => a.name.localeCompare(b.name))
    }
    return { schemas, byName: map }
  }, [tables, query])

  return (
    <div className="bg-card flex h-full flex-col gap-3 rounded-xl border p-3">
      <div className="flex items-center gap-2">
        <div className="relative flex-1">
          <SearchIcon
            aria-hidden
            className="text-muted-foreground absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2"
          />
          <Input
            placeholder="Search tables"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="h-8 pl-8 text-xs"
          />
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Refresh tables"
          onClick={onRefresh}
          disabled={loading}
        >
          <RotateCw className={cn(loading && "animate-spin")} />
        </Button>
      </div>

      <ScrollArea className="h-[560px]">
        {loading && tables.length === 0 ? (
          <SidebarSkeleton />
        ) : tables.length === 0 ? (
          <SidebarEmpty error={error} />
        ) : grouped.schemas.length === 0 ? (
          <p className="text-muted-foreground px-1 py-4 text-center text-xs">
            No tables match &quot;{query}&quot;.
          </p>
        ) : (
          <div className="flex flex-col gap-3 pr-2">
            {grouped.schemas.map((schema) => (
              <div key={schema} className="flex flex-col gap-1">
                <div className="text-muted-foreground sticky top-0 z-10 bg-card px-1 pb-1 text-[10px] font-medium uppercase tracking-wide">
                  {schema}
                </div>
                <ul className="flex flex-col gap-0.5">
                  {grouped.byName.get(schema)?.map((t) => {
                    const isSelected =
                      selected?.schema === t.schema && selected?.name === t.name
                    return (
                      <li key={`${t.schema}.${t.name}`}>
                        <button
                          type="button"
                          onClick={() =>
                            onSelect({ schema: t.schema, name: t.name })
                          }
                          className={cn(
                            "group flex w-full flex-col gap-1 rounded-md px-2 py-1.5 text-left transition-colors",
                            "hover:bg-muted/50",
                            isSelected && "bg-muted",
                          )}
                          aria-pressed={isSelected}
                        >
                          <div className="flex items-center gap-1.5">
                            <span className="text-foreground truncate font-mono text-xs">
                              {t.name}
                            </span>
                            {t.kind !== "table" ? (
                              <Badge
                                variant="outline"
                                className="h-4 px-1 text-[9px] uppercase"
                              >
                                {t.kind}
                              </Badge>
                            ) : null}
                          </div>
                          <div className="flex items-center gap-1.5">
                            <Badge
                              variant="ghost"
                              className="text-muted-foreground h-4 px-1 text-[10px] tabular-nums"
                            >
                              {formatRowCount(t.estimated_rows)} rows
                            </Badge>
                            {t.has_rls ? (
                              <Badge
                                variant="secondary"
                                className="h-4 px-1 text-[10px]"
                              >
                                <ShieldCheck className="size-2.5" />
                                RLS
                              </Badge>
                            ) : null}
                          </div>
                        </button>
                      </li>
                    )
                  })}
                </ul>
              </div>
            ))}
          </div>
        )}
      </ScrollArea>
    </div>
  )
}

function SidebarSkeleton() {
  return (
    <div className="flex flex-col gap-2 px-1">
      {Array.from({ length: 8 }).map((_, i) => (
        <Skeleton key={i} className="h-9 w-full" />
      ))}
    </div>
  )
}

function SidebarEmpty({ error }: { error: string | null }) {
  return (
    <Empty className="border-0 p-2">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Database />
        </EmptyMedia>
        <EmptyTitle className="text-xs">
          {error ? "Couldn't load tables" : "No tables"}
        </EmptyTitle>
        <EmptyDescription className="text-xs">
          {error ? (
            <span className="font-mono">{error}</span>
          ) : (
            "The database has no user tables yet."
          )}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}
