// Storage page — object browser over the storage adapter.

import { PageHeader } from "@/components/layout/page-header"

import { StorageView } from "./_components/storage-view"

export const dynamic = "force-dynamic"

export default function Page() {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Storage"
        description="Browse objects, copy signed URLs, upload, and delete. Backed by the configured storage adapter."
      />
      <StorageView />
    </div>
  )
}
