// SPDX-License-Identifier: Apache-2.0

"use client"

import { Bell, ChevronDown, MoonStar, Search, Sun } from "lucide-react"
import { useTheme } from "next-themes"
import { useEffect, useState } from "react"

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
import { Separator } from "@/components/ui/separator"
import { SidebarTrigger } from "@/components/ui/sidebar"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"

import { api } from "@/lib/api"
import type { AdminAnchors } from "@/lib/api"
import type { StatusState } from "@/lib/home/types"
import { polling } from "@/lib/theme"

// Top bar shell — pinned across every admin page.
//
// Sticky to the top of the SidebarInset so it never scrolls away. Three
// explicit groups separated by visible dividers:
//
//   left:   trigger · wordmark · tenant switcher
//   right1: anchor pills (inbox / cost / health)
//   right2: chrome (search · theme · bell · profile)
//
// All anchor values come from /api/v1/admin/anchors; the bar polls every
// `polling.anchors` ms so the values are always fresh regardless of which
// page the operator is on.

interface TopBarProps {
  initialAnchors: AdminAnchors | null
  tenants: { id: string; name: string }[]
  activeTenantId?: string
  user: { name: string; email: string }
}

export function TopBar({
  initialAnchors,
  tenants,
  activeTenantId,
  user,
}: TopBarProps) {
  const [anchors, setAnchors] = useState<AdminAnchors | null>(initialAnchors)

  useEffect(() => {
    let cancelled = false
    const tick = async () => {
      try {
        const next = await api.admin.anchors.get()
        if (!cancelled) setAnchors(next)
      } catch {
        // Silent — leave stale values up.
      }
    }
    const id = setInterval(tick, polling.anchors)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [])

  const activeTenant = tenants.find((t) => t.id === activeTenantId) ?? tenants[0]

  return (
    <header className="sticky top-0 z-30 flex h-14 shrink-0 items-center gap-stack border-b bg-background/95 px-page-x backdrop-blur supports-[backdrop-filter]:bg-background/80">
      {/* Left group */}
      <div className="flex items-center gap-stack">
        <SidebarTrigger className="-ml-1" />
        <Separator orientation="vertical" className="data-vertical:h-5" />
        <span className="text-body font-semibold tracking-tight text-foreground">
          BackAI
        </span>
        {tenants.length > 0 ? (
          <TenantSwitcher tenants={tenants} activeTenant={activeTenant} />
        ) : (
          <Badge
            variant="outline"
            className="h-6 gap-inline px-pill-x text-meta font-normal text-muted-foreground"
          >
            default tenant
          </Badge>
        )}
      </div>

      {/* Spacer */}
      <div className="flex-1" />

      {/* Right group — anchors */}
      <div className="flex items-center gap-inline">
        <AnchorPill
          label="Inbox"
          value={
            anchors === null
              ? "—"
              : anchors.inbox_pending > 0
                ? String(anchors.inbox_pending)
                : "0"
          }
          status={
            anchors === null
              ? "idle"
              : anchors.inbox_pending > 0
                ? "watch"
                : "ok"
          }
          helpText="Pending approvals across all tenants"
        />
        <AnchorPill
          label="Cost"
          value={
            anchors === null ? "—" : formatAnchorUSD(anchors.cost_today_usd)
          }
          status="idle"
          helpText="Total spend today (UTC)"
        />
        <AnchorPill
          label="Health"
          value={anchors === null ? "—" : anchors.health}
          status={anchors === null ? "idle" : healthStatus(anchors.health)}
          helpText="Runtime dependencies (AgentField, Postgres)"
        />
      </div>

      <Separator orientation="vertical" className="data-vertical:h-5" />

      {/* Right group — chrome */}
      <div className="flex items-center gap-inline">
        <CmdKTrigger />
        <ThemeToggle />
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon"
                aria-label="Notifications"
                className="size-8"
              />
            }
          >
            <Bell className="size-icon-inline text-muted-foreground" aria-hidden />
          </TooltipTrigger>
          <TooltipContent>Notifications</TooltipContent>
        </Tooltip>
        <ProfileMenu user={user} />
      </div>
    </header>
  )
}

function TenantSwitcher({
  tenants,
  activeTenant,
}: {
  tenants: { id: string; name: string }[]
  activeTenant?: { id: string; name: string }
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-inline text-body font-normal text-muted-foreground"
          />
        }
      >
        {activeTenant?.name ?? "All tenants"}
        <ChevronDown className="size-3" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-48">
        <DropdownMenuLabel>Switch tenant</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {tenants.map((t) => (
          <DropdownMenuItem key={t.id}>{t.name}</DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function AnchorPill({
  label,
  value,
  status,
  helpText,
}: {
  label: string
  value: string
  status: StatusState
  helpText: string
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            className="inline-flex h-8 items-center gap-inline rounded-md px-pill-x text-meta transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        }
      >
        <span
          aria-hidden
          className={`inline-block size-icon-dot rounded-pill ${dotClass(status)}`}
        />
        <span className="text-muted-foreground uppercase tracking-wide">
          {label}
        </span>
        <span className="font-medium tabular-nums text-foreground">{value}</span>
      </TooltipTrigger>
      <TooltipContent>{helpText}</TooltipContent>
    </Tooltip>
  )
}

function CmdKTrigger() {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            className="h-8 gap-inline rounded-md text-meta text-muted-foreground"
            aria-label="Open command palette"
          />
        }
      >
        <Search className="size-3.5" aria-hidden />
        Search
        <Badge variant="outline" className="ml-inline px-1.5 text-meta font-mono">
          ⌘K
        </Badge>
      </TooltipTrigger>
      <TooltipContent>Command palette (Cmd+K)</TooltipContent>
    </Tooltip>
  )
}

function ThemeToggle() {
  const { setTheme, resolvedTheme } = useTheme()
  const dark = resolvedTheme === "dark"
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="ghost"
            size="icon"
            aria-label="Toggle theme"
            className="size-8"
            onClick={() => setTheme(dark ? "light" : "dark")}
          />
        }
      >
        {dark ? (
          <Sun className="size-icon-inline text-muted-foreground" aria-hidden />
        ) : (
          <MoonStar className="size-icon-inline text-muted-foreground" aria-hidden />
        )}
      </TooltipTrigger>
      <TooltipContent>{dark ? "Light theme" : "Dark theme"}</TooltipContent>
    </Tooltip>
  )
}

function ProfileMenu({ user }: { user: { name: string; email: string } }) {
  const initials = user.name
    .split(/\s+/)
    .map((s) => s[0])
    .filter(Boolean)
    .slice(0, 2)
    .join("")
    .toUpperCase()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon"
            aria-label="Profile menu"
            className="size-8"
          />
        }
      >
        <span className="inline-flex size-7 items-center justify-center rounded-pill bg-muted text-meta font-medium text-muted-foreground">
          {initials || "OP"}
        </span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-48">
        <DropdownMenuLabel className="flex flex-col">
          <span className="text-body">{user.name}</span>
          <span className="text-meta text-muted-foreground">{user.email}</span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem>Profile</DropdownMenuItem>
        <DropdownMenuItem>Settings</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onClick={() => {
            window.location.href = "/api/auth/sign-out"
          }}
        >
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function dotClass(state: StatusState): string {
  switch (state) {
    case "ok":
      return "bg-success"
    case "watch":
      return "bg-warning"
    case "act":
      return "bg-destructive"
    case "idle":
      return "bg-muted-foreground/40"
  }
}

function healthStatus(value: string): StatusState {
  switch (value) {
    case "healthy":
      return "ok"
    case "degraded":
      return "watch"
    case "down":
      return "act"
    default:
      return "idle"
  }
}

function formatAnchorUSD(n: number): string {
  if (n === 0) return "$0"
  if (n < 1) return `$${n.toFixed(4)}`
  if (n < 100) return `$${n.toFixed(2)}`
  return `$${Math.round(n).toLocaleString()}`
}
