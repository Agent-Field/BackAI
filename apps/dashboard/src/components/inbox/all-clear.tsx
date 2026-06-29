// SPDX-License-Identifier: Apache-2.0

import { Check } from "lucide-react"

// Affirmative empty state. Inbox being empty is GOOD — the page brief is
// explicit about not rendering a sad "no items" message. Tone: positive,
// quiet, dignified.

export function AllClear() {
  return (
    <div className="flex flex-col items-center justify-center gap-stack rounded-md border bg-card px-row-x py-20 text-center">
      <div className="flex size-10 items-center justify-center rounded-pill bg-success/10">
        <Check className="size-icon-inline text-success" aria-hidden />
      </div>
      <h2 className="text-body font-medium text-foreground">All clear</h2>
      <p className="max-w-sm text-meta text-muted-foreground">
        Nothing needs your attention now. New approvals or alerts will
        appear here as they arrive.
      </p>
    </div>
  )
}
