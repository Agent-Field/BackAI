// SPDX-License-Identifier: Apache-2.0

"use client"

// UsersView — end users across all tenants.
//
// Read-only table for now. Membership management lives on the tenant detail
// sheet. Filters: debounced email search + tenant select sourced from
// api.admin.tenants.list().

import * as React from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table"
import { Users as UsersIcon } from "lucide-react"

import { api, ApiError, type Tenant, type User } from "@/lib/api"
import {
  Avatar,
  AvatarFallback,
  AvatarImage,
} from "@/components/ui/avatar"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

import { formatRelative } from "../../../operate/_lib/format"

const TENANT_ALL = "all"

function initials(user: User): string {
  const source = user.name ?? user.email ?? "?"
  const parts = source.split(/[\s@]+/).filter(Boolean)
  if (parts.length === 0) return "?"
  if (parts.length === 1) return parts[0]!.slice(0, 2).toUpperCase()
  return (parts[0]!.charAt(0) + parts[1]!.charAt(0)).toUpperCase()
}

export function UsersView() {
  const [search, setSearch] = React.useState("")
  const [debouncedSearch, setDebouncedSearch] = React.useState("")
  const [tenantFilter, setTenantFilter] = React.useState<string>(TENANT_ALL)
  const [tenants, setTenants] = React.useState<Tenant[]>([])
  const [users, setUsers] = React.useState<User[]>([])
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)

  React.useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(search.trim()), 300)
    return () => clearTimeout(t)
  }, [search])

  // Load tenants once for the filter dropdown.
  React.useEffect(() => {
    void api.admin.tenants
      .list()
      .then((r) => setTenants(r.tenants))
      .catch(() => setTenants([]))
  }, [])

  const fetchUsers = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const list = await api.admin.users.list({
        tenant: tenantFilter === TENANT_ALL ? undefined : tenantFilter,
        search: debouncedSearch || undefined,
      })
      setUsers(list.users)
    } catch (e) {
      const message =
        e instanceof ApiError
          ? `${e.code}: ${e.message}`
          : e instanceof Error
            ? e.message
            : "Failed to load users"
      setError(message)
      setUsers([])
    } finally {
      setLoading(false)
    }
  }, [tenantFilter, debouncedSearch])

  React.useEffect(() => {
    void fetchUsers()
  }, [fetchUsers])

  const columns = React.useMemo<ColumnDef<User>[]>(
    () => [
      {
        id: "avatar",
        header: "",
        cell: ({ row }) => (
          <Avatar size="sm">
            {row.original.avatar_url ? (
              <AvatarImage
                src={row.original.avatar_url}
                alt={row.original.name ?? row.original.email}
              />
            ) : null}
            <AvatarFallback>{initials(row.original)}</AvatarFallback>
          </Avatar>
        ),
      },
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => (
          <span className="text-sm">{row.original.name ?? "—"}</span>
        ),
      },
      {
        id: "email",
        header: "Email",
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.email}</span>
        ),
      },
      {
        id: "created_at",
        header: "Created",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-xs">
            {formatRelative(row.original.created_at)}
          </span>
        ),
      },
    ],
    [],
  )

  const table = useReactTable({
    data: users,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          placeholder="Search by email…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="w-72"
        />
        <Select
          value={tenantFilter}
          onValueChange={(v) => setTenantFilter(v ?? TENANT_ALL)}
        >
          <SelectTrigger className="w-56">
            <SelectValue placeholder="All tenants" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={TENANT_ALL}>All tenants</SelectItem>
            {tenants.map((t) => (
              <SelectItem key={t.id} value={t.id}>
                {t.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <span className="text-muted-foreground ml-auto text-xs">
          {loading
            ? "Loading…"
            : users.length === 0
              ? "No users"
              : `${users.length} user${users.length === 1 ? "" : "s"}`}
        </span>
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
            {loading && users.length === 0 ? (
              <SkeletonRows columns={columns.length} />
            ) : users.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={columns.length} className="p-0">
                  <UsersEmpty
                    error={error}
                    hasFilters={
                      tenantFilter !== TENANT_ALL || debouncedSearch !== ""
                    }
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
    </div>
  )
}

function SkeletonRows({ columns }: { columns: number }) {
  return (
    <>
      {Array.from({ length: 4 }).map((_, i) => (
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

function UsersEmpty({
  error,
  hasFilters,
}: {
  error: string | null
  hasFilters: boolean
}) {
  return (
    <Empty className="border-0">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <UsersIcon />
        </EmptyMedia>
        <EmptyTitle>
          {hasFilters ? "No users match these filters" : "No users yet"}
        </EmptyTitle>
        <EmptyDescription>
          {error ? (
            <>
              The runtime returned:{" "}
              <code className="font-mono">{error}</code>.
            </>
          ) : hasFilters ? (
            "Clear the filters to see all users."
          ) : (
            "Users appear here once someone signs in to a tenant."
          )}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}