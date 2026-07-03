// SPDX-License-Identifier: Apache-2.0

"use client"

import { Bell, Check, ChevronDown, MoonStar, Search, Sun } from "lucide-react"
import { useTheme } from "next-themes"
import Link from "next/link"
import { usePathname, useRouter } from "next/navigation"
import { useEffect, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { CommandPalette, useCommandPalette } from "@/components/layout/command-palette"
import { Button } from "@/components/ui/button"
import { DeltaIndicator } from "@/components/ui/delta-indicator"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
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

// Sticky top bar. Three regions, separated by spacing only — no vertical
// dividers, no profile chrome. The intent: a clear left → right reading
// flow (identity · what needs attention · what to do).
//
//   [trigger]  BackAI  [tenant badge]
//        ──────────  flex spacer  ──────────
//                              [● inbox] [● cost] [● health]   [⌘K search]  [☼] [🔔]
//
// Anchors come from /api/v1/admin/anchors, polled every polling.anchors ms.

interface TopBarProps {
  initialAnchors: AdminAnchors | null
  tenants: { id: string; name: string }[]
}

export function TopBar({ initialAnchors, tenants }: TopBarProps) {
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

  return (
    <header className="sticky top-0 z-30 flex h-12 shrink-0 items-center border-b bg-background/95 px-page-x backdrop-blur supports-[backdrop-filter]:bg-background/80">
      {/* Left: identity */}
      <div className="flex items-center gap-stack">
        <SidebarTrigger className="-ml-1" />
        <span className="text-body font-semibold tracking-tight text-foreground">
          BackAI
        </span>
        {tenants.length > 0 ? (
          <TenantSwitcher tenants={tenants} />
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

      {/* Right: anchors + chrome — no vertical dividers, spacing only */}
      <div className="flex items-center gap-stack">
        <div className="flex items-center gap-inline">
          <AnchorPill
            label="Inbox"
            href="/inbox"
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
                : anchors.inbox_has_critical
                  ? "act"
                  : anchors.inbox_pending > 0
                    ? "watch"
                    : "ok"
            }
            helpText="Pending approvals + active system alerts"
          />
          <AnchorPill
            label="Cost"
            href="/cost"
            value={
              anchors === null ? "—" : formatAnchorUSD(anchors.cost_today_usd)
            }
            status="idle"
            helpText="Total spend today (UTC) vs same window yesterday"
            trailing={
              anchors !== null &&
              anchors.cost_yesterday_same_window_usd > 0 ? (
                <DeltaIndicator
                  current={anchors.cost_today_usd}
                  previous={anchors.cost_yesterday_same_window_usd}
                  semantic="cost"
                />
              ) : null
            }
          />
          <AnchorPill
            label="Health"
            href="/health"
            // C6 — color is signal, not decoration. When healthy the dot
            // already conveys "all good"; hide the word "healthy" and
            // only show status text when something needs attention.
            value={
              anchors === null
                ? "—"
                : anchors.health === "healthy"
                  ? ""
                  : anchors.health
            }
            status={anchors === null ? "idle" : healthStatus(anchors.health)}
            helpText="Runtime dependencies (AgentField, Postgres)"
          />
        </div>
        <div className="flex items-center gap-inline pl-stack">
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
        </div>
      </div>
    </header>
  )
}

// The operator console is cross-tenant by design: Cost, Runs, Health and
// the Tenants list all aggregate every tenant, and a single tenant's
// detail lives at /people/tenants/{id}. So "the current tenant" isn't a
// global filter — it's simply whether you're looking at one tenant's
// drilldown. This switcher reflects that: it shows "All tenants" (→ the
// aggregated Tenants list) everywhere except a drilldown, where it shows
// that tenant, and every row navigates to a real view.
function TenantSwitcher({
  tenants,
}: {
  tenants: { id: string; name: string }[]
}) {
  const pathname = usePathname()
  const router = useRouter()

  const match = pathname.match(/^\/people\/tenants\/([^/]+)/)
  const activeId = match ? decodeURIComponent(match[1]) : null
  const activeTenant = activeId
    ? tenants.find((t) => t.id === activeId)
    : undefined
  const label = activeTenant ? friendlyTenantName(activeTenant.name) : "All tenants"

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="sm"
            className="h-7 max-w-[180px] gap-inline text-body font-normal text-muted-foreground"
          />
        }
      >
        <span className="truncate">{label}</span>
        <ChevronDown className="size-3 shrink-0" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-64">
        <DropdownMenuLabel>View tenant</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => router.push("/people/tenants")}>
          <Check
            className={`size-3.5 shrink-0 ${activeId ? "opacity-0" : "opacity-100"}`}
            aria-hidden
          />
          <span className="flex-1 truncate text-body text-foreground">
            All tenants
          </span>
          <span className="text-meta tabular-nums text-muted-foreground">
            {tenants.length}
          </span>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {tenants.map((t) => (
          <DropdownMenuItem
            key={t.id}
            onClick={() =>
              router.push(`/people/tenants/${encodeURIComponent(t.id)}`)
            }
          >
            <Check
              className={`mt-0.5 size-3.5 shrink-0 ${activeId === t.id ? "opacity-100" : "opacity-0"}`}
              aria-hidden
            />
            <div className="flex min-w-0 flex-1 flex-col gap-tile-tight">
              <span className="truncate text-body text-foreground">
                {friendlyTenantName(t.name)}
              </span>
              <span className="truncate font-mono text-meta text-muted-foreground">
                {t.name}
              </span>
            </div>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// Trim auto-generated slugs (e.g. "Block2 Review block2-review-1781634521674")
// down to just the human-readable prefix. Anything past the first
// kebab-cased slug or numeric suffix is hidden in the chip; the full
// string lives in the dropdown row.
function friendlyTenantName(raw: string): string {
  const trimmed = raw.trim()
  if (!trimmed) return "Tenant"
  // Strip trailing slug ("foo bar foo-bar-12345" -> "foo bar")
  const slugMatch = trimmed.match(/^(.*?)\s+[a-z0-9][a-z0-9-]*-?\d{4,}$/i)
  if (slugMatch && slugMatch[1].trim()) return slugMatch[1].trim()
  // Strip kebab-case suffix that mirrors the prefix in slug form.
  const dupeMatch = trimmed.match(/^(.*?)\s+[a-z0-9][a-z0-9-]+$/)
  if (dupeMatch && dupeMatch[1].trim().length > 3) return dupeMatch[1].trim()
  return trimmed.length > 24 ? `${trimmed.slice(0, 22)}…` : trimmed
}

function AnchorPill({
  label,
  value,
  status,
  helpText,
  href,
  trailing,
}: {
  label: string
  value: string
  status: StatusState
  helpText: string
  href?: string
  trailing?: React.ReactNode
}) {
  const pillClass =
    "inline-flex h-8 items-center gap-inline rounded-md px-pill-x text-meta transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          href ? (
            <Link href={href} className={pillClass} />
          ) : (
            <button type="button" className={pillClass} />
          )
        }
      >
        <span
          aria-hidden
          className={`inline-block size-icon-dot rounded-pill ${dotClass(status)}`}
        />
        <span className="text-muted-foreground uppercase tracking-wide">
          {label}
        </span>
        {value ? (
          <span className="font-medium tabular-nums text-foreground">{value}</span>
        ) : null}
        {trailing}
      </TooltipTrigger>
      <TooltipContent>{helpText}</TooltipContent>
    </Tooltip>
  )
}

function CmdKTrigger() {
  const { open, setOpen } = useCommandPalette()
  return (
    <Tooltip>
      <CommandPalette open={open} onOpenChange={setOpen} />
      <TooltipTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            className="h-8 gap-inline rounded-md text-meta text-muted-foreground"
            aria-label="Open command palette"
            onClick={() => setOpen(true)}
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
  // next-themes can't know the resolved theme during SSR, so the icon it
  // picks on the server won't match the client's first render → React
  // hydration mismatch (and a full client re-render of the subtree). Gate
  // the theme-dependent icon behind a mounted flag: SSR and the first
  // client render both show the neutral placeholder, then we swap in the
  // real icon after mount.
  const [mounted, setMounted] = useState(false)
  useEffect(() => setMounted(true), [])
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
        {!mounted ? (
          <Sun className="size-icon-inline text-muted-foreground opacity-0" aria-hidden />
        ) : dark ? (
          <Sun className="size-icon-inline text-muted-foreground" aria-hidden />
        ) : (
          <MoonStar className="size-icon-inline text-muted-foreground" aria-hidden />
        )}
      </TooltipTrigger>
      <TooltipContent>{dark ? "Light theme" : "Dark theme"}</TooltipContent>
    </Tooltip>
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
