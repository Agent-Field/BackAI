// SPDX-License-Identifier: Apache-2.0

"use client"

import { ArrowUpRight, ChevronDown } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

// Modularity surface for the Cost page (page-design-framework Part 9).
// The Cost data is wrapped from LiteLLM; the footer surfaces the adapter
// pill + a deep link to the LiteLLM admin for virtual-key debugging.
// Renders inside its own bordered strip so it reads as a "page foot"
// affordance rather than a stray hint.

const LITELLM_UI_URL =
  typeof window !== "undefined" && process.env.NEXT_PUBLIC_LITELLM_UI_URL
    ? process.env.NEXT_PUBLIC_LITELLM_UI_URL
    : "http://localhost:4000/ui"

export function AdapterFooter() {
  return (
    <footer className="flex flex-wrap items-center justify-between gap-stack rounded-md border bg-card/40 px-row-x py-row-y text-meta text-muted-foreground">
      <div className="flex items-center gap-inline">
        <span>Cost data powered by</span>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="outline"
                size="sm"
                className="h-7 gap-inline text-meta text-foreground"
              />
            }
          >
            <span
              aria-hidden
              className="inline-block size-icon-dot rounded-pill bg-success"
            />
            <span className="font-medium">LiteLLM</span>
            <ChevronDown className="size-3" aria-hidden />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="min-w-56">
            <DropdownMenuLabel>LiteLLM</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              render={
                <a
                  href={LITELLM_UI_URL}
                  target="_blank"
                  rel="noopener noreferrer"
                />
              }
            >
              Open LiteLLM admin
              <ArrowUpRight className="ml-auto size-3.5" aria-hidden />
            </DropdownMenuItem>
            <DropdownMenuItem render={<a href="/platform/adapters" />}>
              Change LLM adapter
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <span className="text-meta">
        Cost ledger aggregates LiteLLM spend events into
        <code className="ml-inline font-mono">suite_cost_events</code>.
      </span>
    </footer>
  )
}
