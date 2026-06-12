// Database studio.
//
// Server component that pre-fetches the table list so the sidebar renders
// without an additional client roundtrip. All interactive state lives in
// the `<DatabaseStudio />` client component.

import { PageHeader } from "@/components/layout/page-header"
import { api, type DBTable } from "@/lib/api"

import { DatabaseStudio } from "./_components/database-studio"

export const dynamic = "force-dynamic"

export default async function Page() {
  let initialTables: DBTable[] = []
  let initialError: string | null = null
  try {
    const list = await api.db.tables()
    initialTables = list.tables
  } catch (e) {
    initialError =
      e instanceof Error ? e.message : "Failed to load tables"
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Database"
        description="Browse tables, inspect schema and policies, run SQL, and manage memory."
      />
      <DatabaseStudio
        initialTables={initialTables}
        initialError={initialError}
      />
    </div>
  )
}
