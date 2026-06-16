// SPDX-License-Identifier: Apache-2.0

"use client"

import Link from "next/link"
import { usePathname, useRouter } from "next/navigation"
import { useMemo, useState } from "react"
import { useTheme } from "next-themes"
import {
  Activity,
  AlertTriangle,
  Archive,
  BadgeDollarSign,
  Bot,
  Box,
  Building2,
  ChevronDown,
  ChevronRight,
  Circle,
  Clock3,
  Code2,
  Coins,
  CommandIcon,
  Database,
  Flag,
  Gauge,
  Globe2,
  Home,
  KeyRound,
  Layers3,
  Link2,
  ListTree,
  LockKeyhole,
  Mail,
  Menu,
  Moon,
  Network,
  Plug,
  ReceiptText,
  Rocket,
  Search,
  Server,
  Settings2,
  ShieldCheck,
  Sparkles,
  Table2,
  Terminal,
  Users,
  Webhook,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"
import { Button } from "@/components/ui/button"
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut,
} from "@/components/ui/command"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Separator } from "@/components/ui/separator"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { adminTheme } from "@/lib/new-admin/theme"
import {
  allNavItems,
  groupForPath,
  navGroups,
  navItemForPath,
  normalizePath,
  pinnedNavItems,
  type NavIcon,
} from "@/lib/new-admin/navigation"

const icons: Record<NavIcon, React.ComponentType<{ className?: string }>> = {
  activity: Activity,
  alert: AlertTriangle,
  archive: Archive,
  badge: BadgeDollarSign,
  bot: Bot,
  box: Box,
  brand: Layers3,
  building: Building2,
  cache: Archive,
  chart: Gauge,
  clock: Clock3,
  code: Code2,
  coins: Coins,
  database: Database,
  flag: Flag,
  gauge: Gauge,
  globe: Globe2,
  home: Home,
  key: KeyRound,
  link: Link2,
  list: ListTree,
  lock: LockKeyhole,
  mail: Mail,
  network: Network,
  plug: Plug,
  receipt: ReceiptText,
  rocket: Rocket,
  search: Search,
  server: Server,
  settings: Settings2,
  shield: ShieldCheck,
  spark: Sparkles,
  table: Table2,
  terminal: Terminal,
  users: Users,
  webhook: Webhook,
}

export function AdminShell({ children }: { children: React.ReactNode }) {
  const pathname = normalizePath(usePathname())
  const router = useRouter()
  const { resolvedTheme, setTheme } = useTheme()
  const [collapsed, setCollapsed] = useState(false)
  const [commandOpen, setCommandOpen] = useState(false)

  const activeItem = navItemForPath(pathname)
  const activeGroup = groupForPath(pathname)
  const commandItems = useMemo(() => allNavItems, [])

  function go(href: string) {
    setCommandOpen(false)
    router.push(href)
  }

  return (
    <div
      className="min-h-screen bg-background text-foreground"
      style={
        {
          "--admin-sidebar-width": collapsed
            ? adminTheme.chrome.sidebarRailWidth
            : adminTheme.chrome.sidebarWidth,
          "--admin-topbar-height": adminTheme.chrome.topbarHeight,
        } as React.CSSProperties
      }
    >
      <header className="fixed inset-x-0 top-0 z-40 flex h-(--admin-topbar-height) items-center border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/85">
        <div className="flex h-full w-auto items-center gap-2 border-r px-3 transition-[width] duration-150 ease-out md:w-(--admin-sidebar-width)">
          <Sheet>
            <SheetTrigger
              render={
              <Button variant="ghost" size="icon" className="md:hidden">
                <Menu className="size-4" />
                <span className="sr-only">Open navigation</span>
              </Button>
              }
            />
            <SheetContent side="left" className="w-[280px] p-0">
              <SheetHeader className="border-b">
                <SheetTitle>BackAI Studio</SheetTitle>
              </SheetHeader>
              <SidebarContent pathname={pathname} collapsed={false} />
            </SheetContent>
          </Sheet>
          <Button
            variant="ghost"
            size="icon"
            className="hidden md:inline-flex"
            onClick={() => setCollapsed((value) => !value)}
          >
            <Menu className="size-4" />
            <span className="sr-only">Collapse sidebar</span>
          </Button>
          {!collapsed && (
            <Link href="/" className="flex min-w-0 items-center gap-2 text-sm font-semibold">
              <Sparkles className="size-3.5 fill-foreground" />
              <span className="truncate">BackAI Studio</span>
            </Link>
          )}
        </div>

        <div className="flex min-w-0 flex-1 items-center gap-2 px-2 sm:gap-3 sm:px-4">
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
              <Button variant="outline" size="default" className="h-9 gap-1.5 rounded-full px-3">
                <Circle className="size-2 fill-foreground text-foreground" />
                <span className="hidden sm:inline">Platform</span>
                <ChevronDown className="size-3" />
              </Button>
              }
            />
            <DropdownMenuContent align="start">
              <DropdownMenuLabel>Tenant scope</DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem>Platform · all tenants</DropdownMenuItem>
              <DropdownMenuItem>Tenant: acme</DropdownMenuItem>
              <DropdownMenuItem>Tenant: beta-labs</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          <Breadcrumb className="hidden min-w-0 md:block">
            <BreadcrumbList>
              <BreadcrumbItem>
                <BreadcrumbLink href={activeGroup === "Overview" ? "/" : "#"}>{activeGroup}</BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbPage>{activeItem.title}</BreadcrumbPage>
              </BreadcrumbItem>
            </BreadcrumbList>
          </Breadcrumb>

          <button
            type="button"
            className="mx-auto hidden h-9 w-full max-w-sm items-center gap-2 rounded-md border bg-card px-3 text-left text-sm text-muted-foreground shadow-xs transition-colors hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring/40 focus-visible:outline-none lg:flex"
            onClick={() => setCommandOpen(true)}
          >
            <Search className="size-3.5" />
            <span className="flex-1">Search or jump to...</span>
            <kbd className="rounded border bg-muted px-1.5 py-0.5 font-mono text-[11px]">⌘K</kbd>
          </button>

          <div className="ml-auto flex items-center gap-1">
            <Tooltip>
              <TooltipTrigger
                render={
                <Button variant="ghost" size="icon" onClick={() => setCommandOpen(true)}>
                  <CommandIcon className="size-4" />
                  <span className="sr-only">Open command palette</span>
                </Button>
                }
              />
              <TooltipContent>Command palette</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                render={
                <Button
                  variant="ghost"
                  size="icon"
                  className="hidden sm:inline-flex"
                  onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
                >
                  <Moon className="size-4" />
                  <span className="sr-only">Toggle theme</span>
                </Button>
                }
              />
              <TooltipContent>Toggle theme</TooltipContent>
            </Tooltip>
            <Button variant="ghost" size="icon" className="relative hidden sm:inline-flex">
              <Circle className="absolute right-1 top-1 size-1.5 fill-red-400 text-red-400" />
              <Activity className="size-4" />
              <span className="sr-only">Alerts</span>
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                <Button variant="outline" size="icon" className="rounded-full font-mono text-[11px]">
                  OP
                </Button>
                }
              />
              <DropdownMenuContent align="end">
                <DropdownMenuLabel>Operator</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem>Account</DropdownMenuItem>
                <DropdownMenuItem>Sessions</DropdownMenuItem>
                <DropdownMenuItem render={<Link href="/old">Open old dashboard</Link>} />
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </header>

      <aside className="fixed bottom-0 left-0 top-(--admin-topbar-height) z-30 hidden w-(--admin-sidebar-width) border-r bg-background transition-[width] duration-150 ease-out md:block">
        <SidebarContent pathname={pathname} collapsed={collapsed} />
      </aside>

      <main className="min-h-screen pt-(--admin-topbar-height) md:pl-(--admin-sidebar-width)">
        {children}
      </main>

      <CommandDialog open={commandOpen} onOpenChange={setCommandOpen} title="Command palette">
        <Command>
          <CommandInput placeholder="Jump to a page, tenant, run, or action..." />
          <CommandList>
            <CommandEmpty>No command found.</CommandEmpty>
            <CommandGroup heading="Jump to">
              {commandItems.map((item) => {
                const Icon = icons[item.icon]
                return (
                  <CommandItem key={item.href} value={`${item.title} ${item.description}`} onSelect={() => go(item.href)}>
                    <Icon className="size-4" />
                    <span>{item.title}</span>
                    <CommandShortcut>{item.href === "/" ? "home" : item.href}</CommandShortcut>
                  </CommandItem>
                )
              })}
            </CommandGroup>
            <CommandSeparator />
            <CommandGroup heading="Actions">
              {["Issue API key", "Set budget", "Test agent", "Replay webhook", "Open adapter admin"].map((action) => (
                <CommandItem key={action} value={action}>
                  <CommandIcon className="size-4" />
                  <span>{action}</span>
                  <CommandShortcut>drawer</CommandShortcut>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </CommandDialog>
    </div>
  )
}

function SidebarContent({ pathname, collapsed }: { pathname: string; collapsed: boolean }) {
  return (
    <nav className="flex h-full flex-col overflow-hidden">
      <div className="flex-1 overflow-y-auto py-3">
        {navGroups.map((group) => (
          <details key={group.title} open={group.defaultOpen || group.items.some((item) => pathname === item.href)}>
            <summary
              className={cn(
                "mx-2 flex h-8 cursor-pointer list-none items-center gap-2 rounded-md px-2 text-[11px] font-medium uppercase text-muted-foreground transition-colors hover:bg-muted",
                collapsed && "justify-center px-0"
              )}
            >
              {!collapsed && <span className="flex-1">{group.title}</span>}
              {!collapsed && <ChevronRight className="size-3 transition-transform details-open:rotate-90" />}
            </summary>
            <div className="mt-1 space-y-0.5 px-2">
              {group.items.map((item) => (
                <SidebarLink key={item.href} item={item} active={pathname === item.href} collapsed={collapsed} />
              ))}
            </div>
          </details>
        ))}
      </div>

      <Separator />
      <div className="space-y-1 p-2">
        {pinnedNavItems.map((item) => (
          <SidebarLink key={item.href} item={item} active={pathname === item.href} collapsed={collapsed} />
        ))}
        <SidebarLink
          item={{
            title: "Old admin",
            href: "/old",
            icon: "archive",
            description: "Previous dashboard parked under /old.",
          }}
          active={pathname === "/old"}
          collapsed={collapsed}
        />
      </div>
    </nav>
  )
}

function SidebarLink({
  item,
  active,
  collapsed,
}: {
  item: { title: string; href: string; icon: NavIcon; description: string }
  active: boolean
  collapsed: boolean
}) {
  const Icon = icons[item.icon]
  const link = (
    <Link
      href={item.href}
      className={cn(
        "relative flex h-8 items-center gap-2 rounded-md px-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/40 focus-visible:outline-none",
        active && "bg-muted text-foreground before:absolute before:left-0 before:top-1 before:h-6 before:w-0.5 before:rounded-full before:bg-foreground",
        collapsed && "justify-center px-0"
      )}
    >
      <Icon className="size-4 shrink-0" />
      {!collapsed && <span className="truncate">{item.title}</span>}
      {active && !collapsed && <Badge variant="outline" className="ml-auto h-5 rounded-full px-1.5 text-[10px]">live</Badge>}
    </Link>
  )

  if (!collapsed) return link
  return (
    <Tooltip>
      <TooltipTrigger render={link} />
      <TooltipContent side="right">{item.title}</TooltipContent>
    </Tooltip>
  )
}
