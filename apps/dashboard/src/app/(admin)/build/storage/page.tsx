// SPDX-License-Identifier: Apache-2.0

// Storage page — Build → Storage. Objects in the configured storage bucket,
// backed by GET /api/v1/storage. Server component: fetched once per render
// (force-dynamic + no-store via lib/api.ts). When the runtime isn't reachable
// we render a clean empty-state shell rather than crashing.

import { HardDrive } from "lucide-react"

import { api, ApiError, type StorageList } from "@/lib/api"
import { PageHeader } from "@/components/layout/page-header"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

export const dynamic = "force-dynamic"

async function loadStorage(): Promise<{
  data: StorageList
  error: string | null
}> {
  try {
    const data = await api.storage.list()
    return { data, error: null }
  } catch (e) {
    const message =
      e instanceof ApiError
        ? `${e.code}: ${e.message}`
        : e instanceof Error
          ? e.message
          : "Failed to load storage"
    return {
      data: { objects: [], prefix: "", next_token: null },
      error: message,
    }
  }
}

export default async function Page() {
  const { data, error } = await loadStorage()
  const objects = data.objects

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Storage"
        description="Objects in the configured storage bucket. Backed by /api/v1/storage."
      />

      {error && objects.length === 0 ? (
        <div className="bg-card text-muted-foreground rounded-xl border border-dashed p-6 text-sm">
          <p className="text-foreground mb-1 font-medium">
            Storage listing isn&apos;t available
          </p>
          <p>
            The runtime returned:{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {error}
            </code>
            .
          </p>
        </div>
      ) : objects.length === 0 ? (
        <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-xl border p-6 text-xs">
          <HardDrive className="size-3.5" />
          No objects in storage.
        </div>
      ) : (
        <div className="bg-card rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Key
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Type
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Size
                </TableHead>
                <TableHead className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
                  Modified
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {objects.map((object) => (
                <TableRow key={object.key} className="hover:bg-muted/30">
                  <TableCell className="max-w-xs">
                    <code className="block truncate font-mono text-xs">
                      {object.key}
                    </code>
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {object.content_type ?? "—"}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs tabular-nums">
                    {formatBytes(object.size)}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs tabular-nums">
                    {object.last_modified}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {data.next_token !== null ? (
        <p className="text-muted-foreground text-xs">
          Showing the first page of results.
        </p>
      ) : null}
    </div>
  )
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  const value = bytes / Math.pow(1024, i)
  const fixed = value < 10 ? value.toFixed(2) : value.toFixed(1)
  return `${fixed} ${units[i]}`
}
