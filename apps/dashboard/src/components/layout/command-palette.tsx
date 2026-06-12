// SPDX-License-Identifier: Apache-2.0

"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"

import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command"
import { getNavGroupsWithPlugins, NAV_HOME, NAV_SETTINGS } from "@/lib/nav"
import type { NavItem } from "@/lib/nav"

type CommandPaletteProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  billingDisabled?: boolean
  showShipwright?: boolean
}

export function CommandPalette({
  open,
  onOpenChange,
  billingDisabled = false,
  showShipwright = false,
}: CommandPaletteProps) {
  const router = useRouter()
  const [value, setValue] = useState("")
  const navGroups = getNavGroupsWithPlugins({ billingDisabled, showShipwright })

  useEffect(() => {
    if (!open) setValue("")
  }, [open])

  const go = (href: string) => {
    onOpenChange(false)
    router.push(href)
  }

  const navItem = (item: NavItem) => (
    <CommandItem
      key={item.id}
      value={`${item.label} ${item.description ?? ""}`}
      onSelect={() => go(item.href)}
    >
      <item.icon />
      <span>{item.label}</span>
      {item.description ? (
        <span className="text-muted-foreground ml-2 truncate text-xs">{item.description}</span>
      ) : null}
    </CommandItem>
  )

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Navigate"
      description="Jump to any section of the dashboard."
    >
      <CommandInput value={value} onValueChange={setValue} placeholder="Type to search…" />
      <CommandList>
        <CommandEmpty>No results.</CommandEmpty>
        <CommandGroup heading="Overview">{navItem(NAV_HOME)}</CommandGroup>
        {navGroups.map((group) => (
          <div key={group.id}>
            <CommandSeparator />
            <CommandGroup heading={group.label}>{group.items.map(navItem)}</CommandGroup>
          </div>
        ))}
        <CommandSeparator />
        <CommandGroup heading="Settings">{navItem(NAV_SETTINGS)}</CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}

export function useCommandPalette(): {
  open: boolean
  setOpen: (open: boolean) => void
} {
  const [open, setOpen] = useState(false)
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault()
        setOpen((p) => !p)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])
  return { open, setOpen }
}
