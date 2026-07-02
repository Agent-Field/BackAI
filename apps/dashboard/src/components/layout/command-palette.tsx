// SPDX-License-Identifier: Apache-2.0

"use client"

// Command palette behind the top-bar "Search ⌘K" trigger. The trigger
// used to be decorative (no handler, no palette); this wires the
// existing ui/command primitive to the pages that actually exist today.

import * as React from "react"
import { useRouter } from "next/navigation"
import {
  Activity,
  Building2,
  DollarSign,
  HeartPulse,
  Home,
  Inbox,
  LogOut,
} from "lucide-react"

import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command"

const PAGES = [
  { label: "Home", href: "/", icon: Home },
  { label: "Inbox", href: "/inbox", icon: Inbox },
  { label: "Cost", href: "/cost", icon: DollarSign },
  { label: "Health", href: "/health", icon: HeartPulse },
  { label: "Runs", href: "/activity/runs", icon: Activity },
  { label: "Tenants", href: "/people/tenants", icon: Building2 },
]

export function useCommandPalette() {
  const [open, setOpen] = React.useState(false)

  React.useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        setOpen((o) => !o)
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [])

  return { open, setOpen }
}

export function CommandPalette({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const router = useRouter()

  const go = (href: string) => {
    onOpenChange(false)
    router.push(href)
  }

  return (
    <CommandDialog open={open} onOpenChange={onOpenChange}>
      {/* This repo's CommandDialog does NOT wrap children in the cmdk
          root (upstream shadcn does) — without <Command> the input has
          no cmdk store and crashes on mount. */}
      <Command>
        <CommandInput placeholder="Go to page…" />
      <CommandList>
        <CommandEmpty>No results.</CommandEmpty>
        <CommandGroup heading="Pages">
          {PAGES.map((page) => (
            <CommandItem key={page.href} onSelect={() => go(page.href)}>
              <page.icon className="size-4" aria-hidden />
              {page.label}
            </CommandItem>
          ))}
        </CommandGroup>
        <CommandSeparator />
        <CommandGroup heading="Session">
          <CommandItem
            onSelect={() => {
              // Route handler clears the session server-side; full
              // navigation so the middleware re-evaluates.
              window.location.assign("/sign-out")
            }}
          >
            <LogOut className="size-4" aria-hidden />
            Sign out
          </CommandItem>
        </CommandGroup>
      </CommandList>
      </Command>
    </CommandDialog>
  )
}
