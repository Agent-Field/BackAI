"use client"

import { useTheme } from "next-themes"
import { useRouter } from "next/navigation"
import Link from "next/link"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Separator } from "@/components/ui/separator"
import { SidebarTrigger } from "@/components/ui/sidebar"
import { SearchIcon, Moon, Sun, Monitor, LogOut, UserIcon } from "lucide-react"
import { signOut } from "@/lib/auth-client"
import { CommandPalette, useCommandPalette } from "./command-palette"

type TopbarProps = {
  user?: {
    name?: string | null
    email?: string | null
  } | null
}

export function Topbar({ user }: TopbarProps) {
  const router = useRouter()
  const { theme, setTheme } = useTheme()
  const palette = useCommandPalette()

  const initials =
    user?.name?.split(" ").map((s) => s[0]).join("").slice(0, 2).toUpperCase() ||
    user?.email?.[0]?.toUpperCase() ||
    "U"

  const handleSignOut = async () => {
    await signOut()
    router.push("/login")
  }

  return (
    <>
      <header className="bg-background/95 supports-[backdrop-filter]:bg-background/60 sticky top-0 z-10 flex h-14 items-center gap-2 border-b px-4 backdrop-blur">
        <SidebarTrigger />
        <Separator orientation="vertical" className="h-4" />

        <Button
          variant="outline"
          size="sm"
          className="text-muted-foreground ml-2 w-72 justify-start font-normal"
          onClick={() => palette.setOpen(true)}
        >
          <SearchIcon data-icon="inline-start" />
          Jump to…
          <kbd className="bg-muted ml-auto inline-flex h-5 items-center gap-1 rounded border px-1.5 font-mono text-[10px] font-medium">
            ⌘K
          </kbd>
        </Button>

        <div className="ml-auto flex items-center gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon" aria-label="Toggle theme">
                  {theme === "light" ? (
                    <Sun />
                  ) : theme === "dark" ? (
                    <Moon />
                  ) : (
                    <Monitor />
                  )}
                </Button>
              }
            />
            <DropdownMenuContent align="end">
              <DropdownMenuLabel>Theme</DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuItem onClick={() => setTheme("light")}>
                  <Sun /> Light
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setTheme("dark")}>
                  <Moon /> Dark
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setTheme("system")}>
                  <Monitor /> System
                </DropdownMenuItem>
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>

          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon"
                  className="rounded-full"
                  aria-label="User menu"
                >
                  <Avatar className="size-8">
                    <AvatarFallback>{initials}</AvatarFallback>
                  </Avatar>
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuLabel>
                <div className="flex flex-col">
                  <span className="truncate text-sm font-medium">
                    {user?.name ?? "Operator"}
                  </span>
                  <span className="text-muted-foreground truncate text-xs">
                    {user?.email}
                  </span>
                </div>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuItem
                  render={
                    <Link href="/settings/account">
                      <UserIcon /> Account
                    </Link>
                  }
                />
                <DropdownMenuItem render={<Link href="/settings">Settings</Link>} />
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={handleSignOut}>
                <LogOut /> Sign out
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </header>

      <CommandPalette open={palette.open} onOpenChange={palette.setOpen} />
    </>
  )
}
