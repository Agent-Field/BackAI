// SPDX-License-Identifier: Apache-2.0

"use client"

import { Download, MoreHorizontal, UserX } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

import { api } from "@/lib/api"
import type { User } from "@/lib/api"
import { formatAge } from "@/lib/tenant-detail/derive"

// Users table — sticky header + one row per user. The GDPR endpoints
// (export / erase) hide behind a kebab menu so the destructive surface
// stays subtle. Erase double-confirms via the shell's dialog.

const USER_ROW_COLUMNS =
  "grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_110px_110px_60px]"

interface UsersTableProps {
  users: User[]
  healthy: boolean
  onErase: (user: User) => void
}

export function UsersTable({ users, healthy, onErase }: UsersTableProps) {
  return (
    <section
      aria-label="Users table"
      className="flex min-h-0 flex-col rounded-md border bg-card"
    >
      <header
        className={`sticky top-0 z-10 grid items-center gap-stack border-b bg-card px-row-x py-tile-tight text-eyebrow uppercase tracking-wide text-muted-foreground ${USER_ROW_COLUMNS}`}
      >
        <span>Email</span>
        <span>Name</span>
        <span>Created</span>
        <span>State</span>
        <span className="text-right tabular-nums normal-case">
          {users.length}
        </span>
      </header>
      {!healthy ? (
        <DegradedRow />
      ) : users.length === 0 ? (
        <EmptyRow />
      ) : (
        <ul role="list" className="flex flex-col divide-y">
          {users.map((user) => (
            <UserRow key={user.id} user={user} onErase={onErase} />
          ))}
        </ul>
      )}
    </section>
  )
}

function UserRow({
  user,
  onErase,
}: {
  user: User
  onErase: (user: User) => void
}) {
  const [exporting, setExporting] = useState(false)
  const erased = user.deleted_at !== null

  const exportData = async () => {
    if (exporting) return
    setExporting(true)
    try {
      const data = await api.admin.users.exportData(user.id)
      const blob = new Blob([JSON.stringify(data, null, 2)], {
        type: "application/json",
      })
      const url = URL.createObjectURL(blob)
      const a = document.createElement("a")
      a.href = url
      a.download = `user-${user.id}-export.json`
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
      toast.success("Export downloaded", { description: user.email })
    } catch (err) {
      toast.error("Could not export user data", {
        description: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setExporting(false)
    }
  }

  return (
    <li
      className={`grid items-center gap-stack px-row-x py-row-y text-meta ${USER_ROW_COLUMNS}`}
    >
      <span
        className={`truncate text-body font-medium ${
          erased ? "text-muted-foreground line-through" : "text-foreground"
        }`}
        title={user.id}
      >
        {user.email}
      </span>
      <span className="truncate text-muted-foreground">
        {user.name ?? "—"}
      </span>
      <span
        className="font-mono tabular-nums text-muted-foreground"
        title={user.created_at}
      >
        {formatAge(user.created_at)}
      </span>
      <span>
        {erased ? (
          <Badge variant="destructive" title={user.deleted_at ?? undefined}>
            erased
          </Badge>
        ) : (
          <Badge variant="outline">active</Badge>
        )}
      </span>
      <div className="flex justify-end">
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                type="button"
                size="sm"
                variant="ghost"
                aria-label={`Actions for ${user.email}`}
                className="h-7 w-7 p-0 text-muted-foreground"
              />
            }
          >
            <MoreHorizontal className="size-3.5" aria-hidden />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-44">
            <DropdownMenuLabel>GDPR</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem disabled={exporting} onClick={exportData}>
              <Download className="size-3.5" aria-hidden />
              {exporting ? "Exporting…" : "Export data"}
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={erased}
              onClick={() => onErase(user)}
              className="text-destructive"
            >
              <UserX className="size-3.5" aria-hidden />
              Erase…
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </li>
  )
}

function EmptyRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">
        No users match the current filters.
      </p>
      <p className="max-w-md text-meta text-muted-foreground">
        Users appear here after signing up through the customer app or the
        operator dashboard.
      </p>
    </div>
  )
}

function DegradedRow() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack px-row-x py-12 text-center">
      <p className="text-body text-foreground">User list unavailable.</p>
      <p className="max-w-md text-meta text-muted-foreground">
        The runtime did not return a users list. Check the Health page for
        a database probe, then retry.
      </p>
    </div>
  )
}
