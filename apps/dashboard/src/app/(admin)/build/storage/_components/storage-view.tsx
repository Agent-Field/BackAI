"use client"

// StorageView — list, upload, signed-URL, delete.
//
// Pagination uses next_token (cursor) — we keep a stack of seen tokens so
// the user can step back through pages.

import * as React from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import {
  Download,
  HardDrive,
  Link as LinkIcon,
  MoreHorizontal,
  Trash2,
  Upload,
} from "lucide-react"
import { toast } from "sonner"

import { api, ApiError, type StorageObject } from "@/lib/api"
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
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationNext,
  PaginationPrevious,
} from "@/components/ui/pagination"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { cn } from "@/lib/utils"

import { formatRelative } from "../../../operate/_lib/format"

const PAGE_LIMIT = 50

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / Math.pow(1024, i)
  return `${value.toFixed(value >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}
export function StorageView() {
  const [prefix, setPrefix] = React.useState("")
  const [debouncedPrefix, setDebouncedPrefix] = React.useState("")
  const [objects, setObjects] = React.useState<StorageObject[]>([])
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)
  // Pagination: a stack of cursors we've already loaded, plus the current
  // "next" pointer. Index 0 is always the first page (no token).
  const [tokenStack, setTokenStack] = React.useState<(string | null)[]>([null])
  const [pageIndex, setPageIndex] = React.useState(0)
  const [nextToken, setNextToken] = React.useState<string | null>(null)

  const [deleteTarget, setDeleteTarget] = React.useState<StorageObject | null>(
    null,
  )
  const [openUpload, setOpenUpload] = React.useState(false)

  React.useEffect(() => {
    const t = setTimeout(() => setDebouncedPrefix(prefix.trim()), 300)
    return () => clearTimeout(t)
  }, [prefix])

  // Reset pagination when prefix changes.
  React.useEffect(() => {
    setTokenStack([null])
    setPageIndex(0)
    setNextToken(null)
  }, [debouncedPrefix])

  const currentToken = tokenStack[pageIndex] ?? null

  const fetchObjects = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.storage.list({
        prefix: debouncedPrefix || undefined,
        next_token: currentToken || undefined,
        limit: PAGE_LIMIT,
      })
      setObjects(list.objects)
      setNextToken(list.next_token)
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to list storage"
      setError(message)
      setObjects([])
      setNextToken(null)
    } finally {
      setLoading(false)
    }
  }, [debouncedPrefix, currentToken])

  React.useEffect(() => {
    void fetchObjects()
  }, [fetchObjects])

  const goNext = () => {
    if (!nextToken) return
    const newToken = nextToken
    setTokenStack((prev) => {
      // If we already know the next token in the stack, just advance.
      if (prev[pageIndex + 1] === newToken) return prev
      return [...prev.slice(0, pageIndex + 1), newToken]
    })
    setPageIndex((i) => i + 1)
  }

  const goPrev = () => {
    if (pageIndex === 0) return
    setPageIndex((i) => i - 1)
  }

  const handleCopySignedUrl = async (key: string) => {
    try {
      const res = await api.storage.signedURL(key)
      await navigator.clipboard.writeText(res.url)
      toast.success("Signed URL copied", {
        description: `Expires ${formatRelative(res.expires_at)}`,
      })
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to fetch signed URL"
      toast.error("Could not copy URL", { description: message })
    }
  }

  const handleDownload = async (key: string) => {
    try {
      const res = await api.storage.signedURL(key)
      window.open(res.url, "_blank", "noopener,noreferrer")
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to fetch signed URL"
      toast.error("Could not download", { description: message })
    }
  }

  const columns = React.useMemo<ColumnDef<StorageObject>[]>(
    () => [
      {
        id: "key",
        header: "Key",
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.key}</span>
        ),
      },
      {
        id: "size",
        header: "Size",
        cell: ({ row }) => (
          <span className="tabular-nums text-xs">
            {formatBytes(row.original.size)}
          </span>
        ),
      },
      {
        id: "content_type",
        header: "Type",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {row.original.content_type || "—"}
          </span>
        ),
      },
      {
        id: "last_modified",
        header: "Modified",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {formatRelative(row.original.last_modified)}
          </span>
        ),
      },
      {
        id: "actions",
        header: "",
        cell: ({ row }) => (
          <div className="flex justify-end">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button variant="ghost" size="icon-sm" aria-label="Actions">
                    <MoreHorizontal />
                  </Button>
                }
              />
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  onClick={() => void handleCopySignedUrl(row.original.key)}
                >
                  <LinkIcon />
                  Copy signed URL
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => void handleDownload(row.original.key)}
                >
                  <Download />
                  Download
                </DropdownMenuItem>
                <DropdownMenuItem
                  variant="destructive"
                  onClick={() => setDeleteTarget(row.original)}
                >
                  <Trash2 />
                  Delete
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        ),
      },
    ],
    [],
  )

  const table = useReactTable({
    data: objects,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  const hasPrev = pageIndex > 0
  const hasNext = nextToken !== null

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          placeholder="Filter by prefix…"
          value={prefix}
          onChange={(e) => setPrefix(e.target.value)}
          className="w-72"
        />
        <span className="text-muted-foreground text-xs">
          {loading
            ? "Loading…"
            : objects.length === 0
              ? "No objects"
              : `${objects.length} on this page`}
        </span>
        <div className="ml-auto">
          <Button size="sm" onClick={() => setOpenUpload(true)}>
            <Upload />
            Upload
          </Button>
        </div>
      </div>

      <div className="rounded-xl border bg-card">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((hg) => (
              <TableRow key={hg.id} className="hover:bg-transparent">
                {hg.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    className="text-muted-foreground text-xs font-medium uppercase tracking-wide"
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
            {loading && objects.length === 0 ? (
              <SkeletonRows columns={columns.length} />
            ) : objects.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={columns.length} className="p-0">
                  <StorageEmpty
                    prefix={debouncedPrefix}
                    error={error}
                    onUpload={() => setOpenUpload(true)}
                  />
                </TableCell>
              </TableRow>
            ) : (
              table.getRowModel().rows.map((row) => (
                <TableRow key={row.id} className="hover:bg-muted/30">
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      {objects.length > 0 ? (
        <div className="flex items-center justify-between gap-2 px-1">
          <p className="text-muted-foreground text-xs">
            Page {pageIndex + 1}
            {debouncedPrefix ? (
              <>
                {" · prefix "}
                <code className="font-mono">{debouncedPrefix}</code>
              </>
            ) : null}
          </p>
          <Pagination className="m-0 w-fit justify-end">
            <PaginationContent>
              <PaginationItem>
                <PaginationPrevious
                  href="#"
                  className={cn(!hasPrev && "pointer-events-none opacity-50")}
                  onClick={(e) => {
                    e.preventDefault()
                    if (hasPrev) goPrev()
                  }}
                />
              </PaginationItem>
              <PaginationItem>
                <PaginationNext
                  href="#"
                  className={cn(!hasNext && "pointer-events-none opacity-50")}
                  onClick={(e) => {
                    e.preventDefault()
                    if (hasNext) goNext()
                  }}
                />
              </PaginationItem>
            </PaginationContent>
          </Pagination>
        </div>
      ) : null}

      <UploadDialog
        open={openUpload}
        onOpenChange={setOpenUpload}
        defaultPrefix={debouncedPrefix}
        onUploaded={fetchObjects}
      />

      <DeleteObjectDialog
        target={deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        onDeleted={fetchObjects}
      />
    </div>
  )
}

function SkeletonRows({ columns }: { columns: number }) {
  return (
    <>
      {Array.from({ length: 5 }).map((_, i) => (
        <TableRow key={i} className="hover:bg-transparent">
          {Array.from({ length: columns }).map((__, j) => (
            <TableCell key={j}>
              <Skeleton className="h-4 w-24" />
            </TableCell>
          ))}
        </TableRow>
      ))}
    </>
  )
}

function StorageEmpty({
  prefix,
  error,
  onUpload,
}: {
  prefix: string
  error: string | null
  onUpload: () => void
}) {
  return (
    <Empty className="border-0">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <HardDrive />
        </EmptyMedia>
        <EmptyTitle>
          {prefix ? "No objects in this prefix yet" : "No objects yet"}
        </EmptyTitle>
        <EmptyDescription>
          {error ? (
            <>
              The runtime returned:{" "}
              <code className="font-mono">{error}</code>.
            </>
          ) : prefix ? (
            <>
              Nothing under <code className="font-mono">{prefix}</code>. Try a
              different prefix or upload an object.
            </>
          ) : (
            "Upload an object or write to the storage adapter from a job handler to see it here."
          )}
        </EmptyDescription>
      </EmptyHeader>
      <div className="mt-4 flex justify-center">
        <Button size="sm" onClick={onUpload}>
          <Upload />
          Upload
        </Button>
      </div>
    </Empty>
  )
}

function UploadDialog({
  open,
  onOpenChange,
  defaultPrefix,
  onUploaded,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  defaultPrefix: string
  onUploaded: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Upload object</DialogTitle>
          <DialogDescription>
            The file is uploaded directly to the storage adapter.
          </DialogDescription>
        </DialogHeader>
        {open ? (
          <UploadDialogBody
            defaultPrefix={defaultPrefix}
            onClose={() => onOpenChange(false)}
            onUploaded={onUploaded}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function UploadDialogBody({
  defaultPrefix,
  onClose,
  onUploaded,
}: {
  defaultPrefix: string
  onClose: () => void
  onUploaded: () => void
}) {
  const [file, setFile] = React.useState<File | null>(null)
  const [key, setKey] = React.useState("")
  // Tracks whether the user has manually edited the key field — once true,
  // we stop auto-deriving from the file name.
  const [keyTouched, setKeyTouched] = React.useState(false)
  const [submitting, setSubmitting] = React.useState(false)
  const fileInputRef = React.useRef<HTMLInputElement | null>(null)

  const deriveKey = (f: File): string => {
    return defaultPrefix
      ? `${defaultPrefix.replace(/\/$/, "")}/${f.name}`
      : f.name
  }

  const handleFileChange = (f: File | null) => {
    setFile(f)
    if (f && !keyTouched) {
      setKey(deriveKey(f))
    }
  }

  const handleKeyChange = (v: string) => {
    setKey(v)
    setKeyTouched(true)
  }

  const handlePickFile = () => {
    fileInputRef.current?.click()
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!file) {
      toast.error("Pick a file first")
      return
    }
    const trimmedKey = key.trim()
    if (!trimmedKey) {
      toast.error("Key is required")
      return
    }
    setSubmitting(true)
    try {
      await api.storage.upload(trimmedKey, file)
      toast.success("Object uploaded", { description: trimmedKey })
      onClose()
      onUploaded()
    } catch (err) {
      const message =
        err instanceof ApiError
          ? `${err.code}: ${err.message}`
          : err instanceof Error
            ? err.message
            : "Upload failed"
      toast.error("Could not upload", { description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={handleSubmit}>
      <FieldGroup>
        <Field>
          <FieldLabel>File</FieldLabel>
          <input
            ref={fileInputRef}
            type="file"
            className="hidden"
            onChange={(e) => handleFileChange(e.target.files?.[0] ?? null)}
          />
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={handlePickFile}
            >
              Choose file…
            </Button>
            <span className="text-muted-foreground text-xs">
              {file ? (
                <span className="font-mono">
                  {file.name} · {formatBytes(file.size)}
                </span>
              ) : (
                "No file chosen"
              )}
            </span>
          </div>
        </Field>
        <Field>
          <FieldLabel htmlFor="upload-key">Key</FieldLabel>
          <Input
            id="upload-key"
            value={key}
            onChange={(e) => handleKeyChange(e.target.value)}
            placeholder={defaultPrefix ? `${defaultPrefix}/...` : "filename"}
          />
          <FieldDescription>
            The path at which the object will be stored. Defaults to the file
            name within the current prefix.
          </FieldDescription>
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
        <Button type="submit" size="sm" disabled={submitting || !file}>
          {submitting ? "Uploading…" : "Upload"}
        </Button>
      </DialogFooter>
    </form>
  )
}

function DeleteObjectDialog({
  target,
  onOpenChange,
  onDeleted,
}: {
  target: StorageObject | null
  onOpenChange: (open: boolean) => void
  onDeleted: () => void
}) {
  const [submitting, setSubmitting] = React.useState(false)

  const handleDelete = async () => {
    if (!target) return
    setSubmitting(true)
    try {
      await api.storage.delete(target.key)
      toast.success("Object deleted", { description: target.key })
      onOpenChange(false)
      onDeleted()
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to delete"
      toast.error("Could not delete", { description: message })
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
          <AlertDialogTitle>Delete object?</AlertDialogTitle>
          <AlertDialogDescription>
            <span className="font-mono">{target?.key}</span> will be removed
            from storage. This cannot be undone.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={submitting}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={handleDelete}
            disabled={submitting}
          >
            {submitting ? "Deleting…" : "Delete"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
