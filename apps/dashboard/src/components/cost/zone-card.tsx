// SPDX-License-Identifier: Apache-2.0

import { cn } from "@/lib/utils"

// ZoneCard / ZoneCardHeader — shared container shape for every Cost
// page section. Wrapping each zone in a card gives the page a visible
// rhythm against the flat background (matching framework principle "the
// page should feel dense even at zero data") and ensures section
// headers, body padding, and dividers are consistent across zones.

interface ZoneCardProps extends React.HTMLAttributes<HTMLElement> {
  children: React.ReactNode
  className?: string
}

export function ZoneCard({ children, className, ...rest }: ZoneCardProps) {
  return (
    <section
      {...rest}
      className={cn(
        "flex flex-col rounded-md border bg-card/40",
        className,
      )}
    >
      {children}
    </section>
  )
}

interface ZoneCardHeaderProps {
  id?: string
  title: string
  subtitle?: React.ReactNode
  trailing?: React.ReactNode
}

export function ZoneCardHeader({
  id,
  title,
  subtitle,
  trailing,
}: ZoneCardHeaderProps) {
  return (
    <header className="flex items-baseline justify-between gap-stack border-b px-row-x py-row-y">
      <div className="flex min-w-0 items-baseline gap-stack">
        <h2
          id={id}
          className="text-body font-medium tracking-tight text-foreground"
        >
          {title}
        </h2>
        {subtitle ? (
          <span className="truncate text-meta text-muted-foreground">
            {subtitle}
          </span>
        ) : null}
      </div>
      {trailing ? <div className="shrink-0">{trailing}</div> : null}
    </header>
  )
}
