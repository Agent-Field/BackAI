"use client"

// DatabaseStudio — top-level client shell for the DB studio.
//
// Owns the currently-selected (schema, table) pair and the active tab.
// Renders the left sidebar (table list) and the main content area
// (tabs router). Each tab is a focused component below.

import * as React from "react"
import { Database } from "lucide-react"

import { api, ApiError, type DBTable, type DBTableDetail } from "@/lib/api"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"

import { TableSidebar } from "./table-sidebar"
import { DataTab } from "./data-tab"
import { StructureTab } from "./structure-tab"
import { PoliciesTab } from "./policies-tab"
import { SqlRunnerTab } from "./sql-runner-tab"
import { MemoryTab } from "./memory-tab"

export type SelectedTable = { schema: string; name: string }

type DatabaseStudioProps = {
  initialTables: DBTable[]
  initialError: string | null
}

type TabKey = "data" | "structure" | "policies" | "sql" | "memory"

export function DatabaseStudio({
  initialTables,
  initialError,
}: DatabaseStudioProps) {
  const [tables, setTables] = React.useState<DBTable[]>(initialTables)
  const [tablesError, setTablesError] = React.useState<string | null>(
    initialError,
  )
  const [tablesLoading, setTablesLoading] = React.useState(false)
  const [selected, setSelected] = React.useState<SelectedTable | null>(() => {
    if (initialTables.length > 0) {
      const t = initialTables[0]
      return { schema: t.schema, name: t.name }
    }
    return null
  })

  const [tab, setTab] = React.useState<TabKey>("data")

  const [detail, setDetail] = React.useState<DBTableDetail | null>(null)
  const [detailLoading, setDetailLoading] = React.useState(false)
  const [detailError, setDetailError] = React.useState<string | null>(null)

  const refreshTables = React.useCallback(async () => {
    setTablesLoading(true)
    setTablesError(null)
    try {
      const list = await api.db.tables()
      setTables(list.tables)
    } catch (e) {
      setTablesError(
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to load tables",
      )
    } finally {
      setTablesLoading(false)
    }
  }, [])

  // Fetch table detail whenever the selection changes (used by the
  // Structure and Policies tabs; Data tab fetches rows independently).
  // The loading flag is flipped via the request's resolution rather than
  // synchronously inside the effect, to avoid cascading-render warnings.
  React.useEffect(() => {
    if (!selected) return
    const controller = new AbortController()
    let started = true

    api.db
      .table(selected.schema, selected.name)
      .then((d) => {
        if (!started || controller.signal.aborted) return
        setDetail(d)
        setDetailError(null)
        setDetailLoading(false)
      })
      .catch((e: unknown) => {
        if (!started || controller.signal.aborted) return
        setDetail(null)
        setDetailError(
          e instanceof ApiError
            ? `${e.code}: ${e.message}`
            : e instanceof Error
              ? e.message
              : "Failed to load table detail",
        )
        setDetailLoading(false)
      })

    // Show the loading indicator on the next tick so the setState is not
    // synchronous to the effect body.
    const t = setTimeout(() => {
      if (!started || controller.signal.aborted) return
      setDetailLoading(true)
    }, 0)

    return () => {
      started = false
      clearTimeout(t)
      controller.abort()
    }
  }, [selected])

  return (
    <div className="flex min-h-[640px] flex-col gap-4 lg:flex-row">
      <aside className="lg:w-[280px] lg:shrink-0">
        <TableSidebar
          tables={tables}
          loading={tablesLoading}
          error={tablesError}
          selected={selected}
          onSelect={(t) => {
            setSelected(t)
            if (tab === "memory") setTab("data")
          }}
          onRefresh={refreshTables}
        />
      </aside>

      <section className="min-w-0 flex-1">
        <Tabs
          value={tab}
          onValueChange={(v) => setTab(v as TabKey)}
          className="gap-4"
        >
          <TabsList>
            <TabsTrigger value="data">Data</TabsTrigger>
            <TabsTrigger value="structure">Structure</TabsTrigger>
            <TabsTrigger value="policies">Policies</TabsTrigger>
            <TabsTrigger value="sql">SQL Runner</TabsTrigger>
            <TabsTrigger value="memory">Memory</TabsTrigger>
          </TabsList>

          <TabsContent value="data">
            {selected ? (
              <DataTab key={`${selected.schema}.${selected.name}`} table={selected} />
            ) : (
              <SelectATableEmpty />
            )}
          </TabsContent>

          <TabsContent value="structure">
            {selected ? (
              <StructureTab
                key={`structure:${selected.schema}.${selected.name}`}
                detail={detail}
                loading={detailLoading}
                error={detailError}
              />
            ) : (
              <SelectATableEmpty />
            )}
          </TabsContent>

          <TabsContent value="policies">
            {selected ? (
              <PoliciesTab
                key={`policies:${selected.schema}.${selected.name}`}
                detail={detail}
                loading={detailLoading}
                error={detailError}
              />
            ) : (
              <SelectATableEmpty />
            )}
          </TabsContent>

          <TabsContent value="sql">
            <SqlRunnerTab />
          </TabsContent>

          <TabsContent value="memory">
            <MemoryTab />
          </TabsContent>
        </Tabs>
      </section>
    </div>
  )
}

function SelectATableEmpty() {
  return (
    <Empty className="border-dashed">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Database />
        </EmptyMedia>
        <EmptyTitle>Select a table</EmptyTitle>
        <EmptyDescription>
          Choose a table from the sidebar to inspect rows, columns, indexes, and
          row-level security policies.
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}
