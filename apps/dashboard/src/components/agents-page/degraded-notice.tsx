// SPDX-License-Identifier: Apache-2.0

import { AlertTriangle } from "lucide-react"

// Server-rendered degraded notice — same warning shape as the Cost /
// Inbox banners, minus the retry button (this page has no client shell;
// a browser reload is the retry).

export function DegradedNotice({ children }: { children: React.ReactNode }) {
  return (
    <div
      role="status"
      className="flex items-center gap-stack rounded-md border border-warning/40 bg-warning/5 px-row-x py-row-y text-body text-foreground"
    >
      <AlertTriangle
        className="size-icon-inline shrink-0 text-warning"
        aria-hidden
      />
      <span className="flex-1">{children}</span>
    </div>
  )
}
